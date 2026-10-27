package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/model"
)

// retryRemoveAll видаляє теку з кількома спробами. На Windows каталог
// можуть тимчасово тримати відкриті файли (антивірус, Провідник, щойно
// зупинений процес гри) — os.RemoveAll тоді падає, і папка збірки
// лишається «напіввидаленою». Чекаємо трохи і пробуємо ще раз.
func retryRemoveAll(dir string) error {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if err := os.RemoveAll(dir); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil // вже видалено (гонка з іншим очищувачем)
		}
		time.Sleep(300 * time.Millisecond)
	}
	return lastErr
}

// ── Архітектура збірок: фронтенд-біндінг ────────────────────────────────
// GetBuilds повертає ВСІ збірки (Шаурма-каталог + локальні кастомні) зі
// станами для ПОТОЧНОГО акаунта.
//
// Семантика станів:
//   - Стан ФАЙЛІВ (not-installed / needs-update / ready) — спільний для всіх
//     акаунтів: файли збірки одні й ті ж, звідки не запускай.
//   - downloading / updating — поточне завантаження (з dlTransient, що
//     наповнюється recordDownloadProgress).
//   - running — збірка запущена на ПОТОЧНОМУ акаунті.
//   - RunningOn — збірка запущена на ІНШОМУ акаунті: стан файлів лишається,
//     але sidebar показує іконку голови скіна акаунта, на якому вона працює.
func (a *App) GetBuilds() []builds.View {
	acc := a.cfg.ActiveAccount()
	var accID string
	if acc.ID != "" {
		accID = acc.ID
	}

	// Каталог Шаурма-збірок — з дискового кешу, БЕЗ очікування мережі (див.
	// packindex_cache.go): раніше старт UI висів, поки відповість воркер.
	var index []api.PackIndexEntry = a.packIndexForView()

	views := make([]builds.View, 0, len(index))
	seen := make(map[string]bool, len(index))
	for i := range index {
		views = append(views, a.buildView(index[i], accID))
		seen[index[i].ID] = true
	}

	// Кастомні збірки (створені вручну або імпортовані з .mrpack/.zip) —
	// зі свого окремого реєстру custompack.Store, об'єднуємо в той самий
	// список, щоб UI (сайдбар, "Мої збірки", вкладка "Кастомні") бачив
	// їх як звичайні View поруч із Shaurma-збірками.
	// Пропускаємо збірки, які вже є в Shaurma-каталозі (не показуємо
	// як кастомні).
	// Вимкнені в налаштуваннях авто-збірки ховаються зі списків (але
	// лишаються на диску; увімкнення показує їх знову).
	autoHidden := a.autoPacksDisabledSet()
	if a.customPacks != nil {
		for _, cp := range a.customPacks.List() {
			if seen[cp.ID] || autoHidden[cp.ID] {
				continue
			}
			views = append(views, a.customBuildView(cp, accID))
			seen[cp.ID] = true
		}
	}

	// Збірки з buildsRegistry, яких немає в жодному з двох списків вище
	// (автоматично знайдені ScanAndRegisterInstances, але не в customPacks
	// і не в Shaurma-каталозі). Показуємо з дефолтними параметрами.
	if a.buildsReg != nil {
		for _, id := range a.buildsReg.AllIDs() {
			if seen[id] || autoHidden[id] {
				continue
			}
			a.addOrphanedBuild(&views, id, accID)
		}
	}

	return views
}

// addOrphanedBuild додає збірку, знайдену на диску (instance.json + buildsRegistry),
// але відсутню в Shaurma-каталозі та customPacks.
func (a *App) addOrphanedBuild(views *[]builds.View, packID, accID string) {
	// Визначаємо Kind з buildsRegistry
	entryKind := builds.KindCustom
	if entry, ok := a.buildsReg.Get(packID); ok && entry.Kind == builds.KindShaurma {
		entryKind = builds.KindShaurma
	}

	instDir := a.packDir(packID, string(entryKind))
	cfgPath := filepath.Join(instDir, "instance.json")
	var icfg model.InstanceConfig

	if data, err := os.ReadFile(cfgPath); err == nil {
		_ = json.Unmarshal(data, &icfg)
	}
	if icfg.ID == "" {
		icfg.ID = packID
	}
	if icfg.Name == "" {
		icfg.Name = packID
	}

	mcDir := filepath.Join(instDir, ".minecraft")
	installed := dirExists(mcDir)
	changed := false

	// Loader завжди дефолтує на vanilla
	if icfg.Loader == "" {
		icfg.Loader = "vanilla"
		changed = true
	}

	// Спроба детекції MCVersion/Loader з .minecraft якщо версія порожня
	if icfg.MCVersion == "" {
		if mcV, ld, ldV := detectMinecraftInfo(mcDir); mcV != "" {
			icfg.MCVersion = mcV
			if icfg.Loader == "" || icfg.Loader == "vanilla" {
				icfg.Loader = ld
			}
			icfg.LoaderVersion = ldV
			changed = true
			log.Printf("builds: авто-детекція %s → MC=%s loader=%s %s", packID, mcV, ld, ldV)
		}
	}

	// Авто-знаходження icon.png / background.png поруч із .minecraft
	if icfg.IconPath == "" {
		for _, name := range []string{"icon.png", "pack.png"} {
			p := filepath.Join(instDir, name)
			if _, err := os.Stat(p); err == nil {
				icfg.IconPath = name
				changed = true
				break
			}
		}
	}
	if icfg.BackgroundPath == "" {
		p := filepath.Join(instDir, "background.png")
		if _, err := os.Stat(p); err == nil {
			icfg.BackgroundPath = "background.png"
			changed = true
		}
	}

	// Зберігаємо оновлений instance.json
	if changed {
		data, _ := json.MarshalIndent(icfg, "", "  ")
		tmpPath := cfgPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err == nil {
			_ = os.Rename(tmpPath, cfgPath)
		}
	}

	build := builds.Build{
		ID:            packID,
		Kind:          entryKind,
		Name:          icfg.Name,
		MCVersion:     icfg.MCVersion,
		Loader:        icfg.Loader,
		LoaderVersion: icfg.LoaderVersion,
		Color:         icfg.Color,
		Description:   icfg.Description,
		Source:        icfg.Source,
	}

	if icfg.IconPath != "" {
		absPath := filepath.Join(instDir, icfg.IconPath)
		if _, err := os.Stat(absPath); err == nil {
			build.IconURL = "local-file://" + absPath
		}
	}
	if icfg.BackgroundPath != "" {
		absPath := filepath.Join(instDir, icfg.BackgroundPath)
		if _, err := os.Stat(absPath); err == nil {
			build.BackgroundURL = "local-file://" + absPath
		}
	}

	var status builds.Status
	if installed {
		status = builds.StatusReady
	} else {
		status = builds.StatusNotInstalled
	}
	v := builds.View{
		Build:     build,
		Status:    status,
		Installed: installed,
	}
	// Перевіряємо чи збірка запущена
	if a.sessionsM != nil {
		for _, s := range a.sessionsM.Snapshot() {
			if s.BuildID == packID {
				if s.AccountID != accID {
					v.RunningOn = append(v.RunningOn, builds.RunningAccount{
						AccountID:   s.AccountID,
						AccountName: s.AccountName,
					})
				} else {
					v.Status = builds.StatusRunning
				}
			}
		}
	}

	// Реєструємо збірку в customPacks ТІЛЬКИ якщо це НЕ Shaurma-збірка.
	// Shaurma-збірки мають свій каталог і ніколи не стають кастомними.
	if a.customPacks != nil && entryKind == builds.KindCustom {
		if _, exists := a.customPacks.Get(packID); !exists {
			pack := custompack.Pack{
				ID:             packID,
				Name:           icfg.Name,
				Description:    icfg.Description,
				MCVersion:      icfg.MCVersion,
				Loader:         icfg.Loader,
				LoaderVersion:  icfg.LoaderVersion,
				Color:          icfg.Color,
				IconPath:       icfg.IconPath,
				BackgroundPath: icfg.BackgroundPath,
				Source:         icfg.Source,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			if err := a.customPacks.Save(pack); err != nil {
				log.Printf("builds: помилка реєстрації %s в customPacks: %v", packID, err)
			}
		}
	}

	*views = append(*views, v)
}

// dirExists перевіряє чи існує і є директорією.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectMinecraftInfo автоматично визначає MCVersion та Loader.
// Якщо є .minecraft/versions/ — шукаємо profile JSON файли.
// Якщо нічого не знайдено або versions/ немає — повертаємо "vanilla"
// як loader, щоб instance.json НІКОЛИ не лишався без лоадера.
func detectMinecraftInfo(mcDir string) (mcVersion, loader, loaderVersion string) {
	loader = "vanilla" // завжди дефолт

	versionsDir := filepath.Join(mcDir, "versions")
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		profileID := strings.TrimSuffix(name, ".json")
		lower := strings.ToLower(profileID)

		if strings.HasPrefix(lower, "fabric-loader-") {
			loader = "fabric"
			parts := strings.SplitN(profileID, "-", 4)
			if len(parts) >= 4 {
				loaderVersion = parts[2]
				mcVersion = parts[3]
			}
			return
		}
		if strings.HasPrefix(lower, "quilt-loader-") {
			loader = "quilt"
			parts := strings.SplitN(profileID, "-", 4)
			if len(parts) >= 4 {
				loaderVersion = parts[2]
				mcVersion = parts[3]
			}
			return
		}
		if strings.HasPrefix(lower, "neoforge-") {
			loader = "neoforge"
			parts := strings.SplitN(profileID, "-", 2)
			if len(parts) >= 2 {
				loaderVersion = parts[1]
			}
			mcVersion = detectMCVersionFromJSON(filepath.Join(versionsDir, name))
			return
		}
		if strings.HasPrefix(lower, "forge-") {
			loader = "forge"
			parts := strings.SplitN(profileID, "-", 2)
			if len(parts) >= 2 {
				loaderVersion = parts[1]
			}
			mcVersion = detectMCVersionFromJSON(filepath.Join(versionsDir, name))
			return
		}
		if strings.Contains(lower, "-forge-") {
			loader = "forge"
			idx := strings.Index(lower, "-forge-")
			loaderVersion = profileID[idx+7:]
			mcVersion = profileID[:idx]
			return
		}
		if strings.HasPrefix(profileID, "1.") || strings.HasPrefix(profileID, "2.") {
			mcVersion = profileID
			return
		}
	}
	return
}

// detectMCVersionFromJSON читає inheritsFrom з profile JSON (Forge/NeoForge).
func detectMCVersionFromJSON(jsonPath string) string {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return ""
	}
	var profile struct {
		InheritsFrom string `json:"inheritsFrom"`
	}
	if json.Unmarshal(data, &profile) != nil {
		return ""
	}
	return profile.InheritsFrom
}

// DeletePack повністю видаляє збірку з локальної машини (ануінстал):
// скасовує активну качку, зупиняє гру на цій збірці, видаляє теку
// установки, запис реєстру, per-instance конфіг і стан синхронізації.
// Сама збірка лишається в каталозі Шаурма — її можна скачати знову.
func (a *App) DeletePack(packID string) error {
	if packID == "" {
		return nil
	}
	if err := a.shareLockErr("build", packID); err != nil {
		return err
	}
	// Активну синхронізацію скасовуємо, щоб рушій не писав у теку після
	// її видалення.
	if a.syncMgr != nil {
		a.syncMgr.Cancel(packID)
	}
	// Гру на цій збірці зупиняємо (на будь-якому акаунті).
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == packID {
			_ = a.sessionsM.Stop(s.AccountID, s.BuildID)
		}
	}
	// Fallback: сесія може бути НЕ в реєстрі, але гра реально йде — напр.
	// відновлена після рестарту лаунчера з видаленим акаунтом (раніше така
	// сесія не реєструвалась у менеджері). Добиваємо процеси з
	// running-game.json по PID і чистимо записи цієї збірки, щоб після
	// видалення вона не «воскресала» при наступному старті лаунчера
	// (restoreRunningGame побачив би живий PID і знову показав би збірку
	// як запущену — фантомна сесія).
	a.runningGamesMu.Lock()
	stale := a.readRunningGamesLocked()
	a.runningGamesMu.Unlock()
	for _, e := range stale {
		if e.BuildID == packID {
			killProcess(e.PID)
			a.clearRunningGame(e.BuildID, e.AccountID)
		}
	}

	// Активний імпорт модпака (браузер/файл) у цю збірку скасовуємо ПЕРЕД
	// видаленням теки: інакше фоновий конвеєр докачував би архів/розпаковував
	// файли у вже видалену теку, і вона «воскресала» частковим сміттям
	// (саме це і бачив ужиток: папка збірки не видалялась повністю).
	a.CancelModpackImport(packID)

	// Тека установки. Помилка RemoveAll НЕ перериває очищення реєстру:
	// файли можуть бути заблоковані (відкрита гра/провідник), але реєстр,
	// per-instance конфіг і стан синхронізації все одно мають забути про
	// збірку — інакше вона лишиться «напівпримарною» (файлів немає, а
	// система досі вважає її встановленою).
	//
	// Видаляємо з ОБОХ шляхів: актуального (cfg.InstancesDir() — тека, яку
	// лаунчер реально використовує зараз) і легасі (settings.InstanceDir з
	// БД, який міг лишитись застарілим після зміни папки даних чи
	// перенесення в launcher-location.json). Якщо видаляти лише один,
	// теку збірки можна було «не помітити» і вона залишалась сміттям на
	// диску.
	// Видаляємо з ВСІХ можливих шляхів (custom + shaurma + легасі).
	paths := map[string]bool{
		a.packDir(packID, string(builds.KindCustom)):  true,
		a.packDir(packID, string(builds.KindShaurma)): true,
		// Legacy: settings.InstanceDir міг лишитись застарілим
		filepath.Join(a.cfg.GetSettings().InstanceDir, packID): true,
	}
	for instDir := range paths {
		if instDir == "" {
			continue
		}
		if err := retryRemoveAll(instDir); err != nil {
			log.Printf("DeletePack: не вдалось видалити теку %s після повторних спроб: %v", instDir, err)
		}
	}

	// Реєстр встановлених збірок + per-instance конфіг + стан синхронізації.
	a.buildsReg.Remove(packID)
	_ = a.cfg.DeleteInstanceConfig(packID)
	// Авто-прибирання версій Minecraft, які більше не потрібні (якщо
	// увімкнено CleanupAfterPackDelete): після видалення збірки її версія
	// (і версії-батьки) видаляються, якщо їх не використовує жодна інша
	// збірка. Це звільняє місце у minecraft/versions, libraries та assets.
	// ВАЖЛИВО: викликаємо ТІЛЬКИ після buildsReg.Remove(packID) — інакше
	// GetBuilds() досі вважав би збірку встановленою, і її версія ніколи б
	// не потрапила до «невикористовуваних» (рев'ю знайшло цей баг).
	if a.cfg.GetSettings().CleanupAfterPackDelete {
		a.CleanupVersionsAfterPackDelete()
	} else {
		// Розмір все одно міг змінитися (installations) — скидаємо кеш, щоб
		// вкладка «Пам'ять і кеш» не показувала застарілі цифри.
		invalidateStorageSizes()
	}
	if st := a.syncStateStore(); st != nil {
		_ = st.Delete(packID)
	}
	// Кастомна збірка (створена вручну/імпортована) — прибираємо й запис
	// опису з custompack.Store. Для Shaurma-збірок Get поверне (Pack{},
	// false), тому цей виклик безпечний завжди.
	if a.customPacks != nil {
		if _, ok := a.customPacks.Get(packID); ok {
			_ = a.customPacks.Delete(packID)
		}
	}
	// Якщо це була авто-збірка — забуваємо її запис (щоб налаштування не
	// показували рядок для видаленої збірки, а закладки «Моїх збірок» не
	// лишили сироту). Повторно авто-збірки не створюються (CreatedOnce).
	a.forgetAutoPackID(packID)

	// Transient-прогрес прибираємо.
	a.dlTransientMu.Lock()
	delete(a.dlTransient, packID)
	a.dlTransientMu.Unlock()

	// Ярлик на робочому столі (якщо створювався): для КАСТОМНИХ збірок
	// видалення = збірка зникає зовсім, ярлик більше не потрібен. Для
	// Shaurma-збірок «Видалити» = лише прибрати файли з диска, а збірка
	// лишається в каталозі (її можна скачати знову) — ярлик ТЕЖ лишається,
	// щоб не зникав з робочого столу після деінсталяції файлів.
	if a.isCustomPack(packID) {
		_ = a.RemoveDesktopShortcut(packID)
	}

	a.emit("builds:changed", packID)
	return nil
}

// buildView будує View однієї Шаурма-збірки для вказаного акаунта.
func (a *App) buildView(e api.PackIndexEntry, accID string) builds.View {
	build := builds.Build{
		ID:            e.ID,
		Kind:          builds.KindShaurma,
		Name:          e.Name,
		Description:   e.Description,
		MCVersion:     e.MCVersion,
		Loader:        e.LoaderType,
		LoaderVersion: e.LoaderVersion,
		IconURL:       e.IconURL,
		BackgroundURL: e.BackgroundURL,
		Color:         e.Color,
		Version:       firstNonEmpty(e.Version, e.UpdatedAt),
		Source:        e.MrpackURL,
		Tags:          e.Tags,
		// ServerIP — офіційний сервер збірки (packs-index.json → serverIp).
		// Без цього віджет офіційного сервера на сторінці збірки ніколи не
		// бачив адресу: GetBuilds()/buildView() губив це поле, хоча
		// api.PackIndexEntry.ServerIP уже приходило коректно з worker-а.
		ServerIP:       e.ServerIP,
		FolderID:       e.FolderID,
		FolderName:     e.FolderName,
		FolderColor:    e.FolderColor,
		FolderOrder:    e.FolderOrder,
		FolderPosition: e.FolderPosition,
	}

	entry, installed := a.buildsReg.Get(e.ID)

	status := builds.StatusReady
	var progress *builds.Progress
	a.dlTransientMu.Lock()
	if p, ok := a.dlTransient[e.ID]; ok {
		progress = p
		if installed {
			status = builds.StatusUpdating
		} else {
			status = builds.StatusDownloading
		}
	} else {
		status = builds.ComputeFileStatus(installed, entry.InstalledVersion, build.Version)
	}
	a.dlTransientMu.Unlock()

	view := builds.View{
		Build:     build,
		Status:    status,
		Installed: installed,
		Progress:  progress,
	}

	// Запуск на акаунтах: стан файлів не змінюємо, але позначаємо, хто
	// і де грає. Поточний акаунт → Status=running (кнопка «Стоп»); решта
	// акаунтів → у RunningOn, щоб sidebar/картка/сторінка показали голови
	// тих, у кого ця збірка запущена (1 збірка × 1 акаунт, акаунтів може
	// бути кілька).
	for _, s := range a.sessionsM.RunningOnBuildAccounts(e.ID) {
		if s.AccountID == accID {
			view.Status = builds.StatusRunning
			// Час старту сесії ПОТОЧНОГО акаунта — фронтенд відновлює
			// таймер «у сесії» (кнопка «Стоп») навіть без події build:started.
			view.StartedAtMs = s.StartedAt.UnixMilli()
		} else {
			view.RunningOn = append(view.RunningOn, builds.RunningAccount{AccountID: s.AccountID, AccountName: s.AccountName})
		}
	}
	return view
}

// customBuildView будує View однієї кастомної (не-Shaurma) збірки для
// вказаного акаунта — дзеркалить buildView, але джерело даних не
// api.PackIndexEntry (сервер), а локальний custompack.Pack.
func (a *App) customBuildView(cp custompack.Pack, accID string) builds.View {
	// Kind визначається buildsRegistry, а НЕ типом виклику: якщо збірка
	// є в Shaurma-реєстрі — вона Shaurma, навіть якщо потрапила в customPacks.
	kind := builds.KindCustom
	if a.buildsReg != nil {
		if entry, ok := a.buildsReg.Get(cp.ID); ok && entry.Kind == builds.KindShaurma {
			kind = builds.KindShaurma
		}
	}
	build := builds.Build{
		ID:            cp.ID,
		Kind:          kind,
		Name:          cp.Name,
		Description:   cp.Description,
		MCVersion:     cp.MCVersion,
		Loader:        cp.Loader,
		LoaderVersion: cp.LoaderVersion,
		Icon:          cp.Icon,
		Color:         cp.Color,
		Source:        cp.Source,
		// Джерело «інтернет-збірки» — для кнопки «Оновити» зі списком
		// версій на сторінці збірки (modrinth/curseforge).
		SourceProjectID: cp.SourceProjectID,
		SourceVersionID: cp.SourceVersionID,
	}
	if cp.IconPath != "" {
		// Локальна кастомна іконка: розв'язуємо відносний шлях динамічно.
		build.IconURL = a.packAssetURL(cp, cp.IconPath)
	}
	if cp.BackgroundPath != "" {
		build.BackgroundURL = a.packAssetURL(cp, cp.BackgroundPath)
	}

	entry, installed := a.buildsReg.Get(cp.ID)

	status := builds.StatusReady
	var progress *builds.Progress
	a.dlTransientMu.Lock()
	if p, ok := a.dlTransient[cp.ID]; ok {
		progress = p
		if installed {
			status = builds.StatusUpdating
		} else {
			status = builds.StatusDownloading
		}
	} else if !installed && cp.ImportState != nil {
		// Незавершений імпорт модпаку (пауза/крах/рестарт лаунчера ДО
		// повного завершення): відновлюємо стан «Пауза» з персистентного
		// Pack.ImportState (custom-packs.json), щоб картка одразу показала
		// «Продовжити» з реальним %, а не порожнє «Не встановлено». Сам
		// імпорт відновить ResumeModpackImport: .part докачається через
		// Range, уже скачані моди й розпаковані overrides пропустяться.
		st := cp.ImportState
		progress = &builds.Progress{
			Percent:    percentOf(st.BytesDownloaded, st.BytesTotal, st.FilesCompleted, st.FilesTotal),
			BytesDone:  st.BytesDownloaded,
			BytesTotal: st.BytesTotal,
			FilesDone:  st.FilesCompleted,
			FilesTotal: st.FilesTotal,
			Stage:      st.Stage,
			Mode:       "paused",
		}
		status = builds.StatusDownloading
	} else if !installed {
		// Кастомна збірка щойно створена вручну (не імпортована) — файлів
		// гри ще немає, це нормальний стан "не встановлено", а не помилка.
		status = builds.StatusNotInstalled
	} else {
		// Для авто-збірок («Vanilla latest» / «Snapshot») файли мають
		// відповідати ПОТОЧНІЙ версії конфіга: поки в реєстрі записана
		// старіша версія (Mojang випустила нову, конфіг перемкнувся сам) —
		// статус NeedsUpdate, картка показує «є нова версія → Завантажити».
		status = builds.ComputeFileStatus(installed, entry.InstalledVersion, a.autoFileTarget(cp))
	}
	a.dlTransientMu.Unlock()

	view := builds.View{
		Build:     build,
		Status:    status,
		Installed: installed,
		Progress:  progress,
	}

	for _, s := range a.sessionsM.RunningOnBuildAccounts(cp.ID) {
		if s.AccountID == accID {
			view.Status = builds.StatusRunning
			// Час старту сесії ПОТОЧНОГО акаунта — фронтенд відновлює
			// таймер «у сесії» (кнопка «Стоп») навіть без події build:started.
			view.StartedAtMs = s.StartedAt.UnixMilli()
		} else {
			view.RunningOn = append(view.RunningOn, builds.RunningAccount{AccountID: s.AccountID, AccountName: s.AccountName})
		}
	}
	return view
}

// firstNonEmpty повертає перший непорожній рядок зі списку.
func firstNonEmpty(parts ...string) string {
	for _, p := range parts {
		if p != "" {
			return p
		}
	}
	return ""
}

// packAssetURL повертає "local-file://..." URL для відносного шляху
// (icon/background) кастомної збірки. Якщо шлях порожній — порожній рядок.
func (a *App) packAssetURL(cp custompack.Pack, relPath string) string {
	if relPath == "" {
		return ""
	}
	return "local-file://" + filepath.Join(a.packDir(cp.ID, string(builds.KindCustom)), relPath)
}
