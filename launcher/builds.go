package main

import (
	"log"
	"os"
	"path/filepath"

	"shaurma-launcher-wails/internal/api"
	"shaurma-launcher-wails/internal/builds"
	"shaurma-launcher-wails/internal/custompack"
)

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

	var index []api.PackIndexEntry
	if a.packs != nil {
		if list, err := a.packs.GetPackIndex(); err == nil {
			index = list
		}
	}

	views := make([]builds.View, 0, len(index))
	for i := range index {
		views = append(views, a.buildView(index[i], accID))
	}

	// Кастомні збірки (створені вручну або імпортовані з .mrpack/.zip) —
	// зі свого окремого реєстру custompack.Store, об'єднуємо в той самий
	// список, щоб UI (сайдбар, "Мої збірки", вкладка "Кастомні") бачив
	// їх як звичайні View поруч із Shaurma-збірками.
	if a.customPacks != nil {
		for _, cp := range a.customPacks.List() {
			views = append(views, a.customBuildView(cp, accID))
		}
	}

	return views
}

// DeletePack повністю видаляє збірку з локальної машини (ануінстал):
// скасовує активну качку, зупиняє гру на цій збірці, видаляє теку
// установки, запис реєстру, per-instance конфіг і стан синхронізації.
// Сама збірка лишається в каталозі Шаурма — її можна скачати знову.
func (a *App) DeletePack(packID string) error {
	if packID == "" {
		return nil
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

	// Тека установки. Помилка RemoveAll НЕ перериває очищення реєстру:
	// файли можуть бути заблоковані (відкрита гра/провідник), але реєстр,
	// per-instance конфіг і стан синхронізації все одно мають забути про
	// збірку — інакше вона лишиться «напівпримарною» (файлів немає, а
	// система досі вважає її встановленою).
	settings := a.cfg.GetSettings()
	instDir := filepath.Join(settings.InstanceDir, packID)
	if err := os.RemoveAll(instDir); err != nil {
		log.Printf("DeletePack: не вдалось видалити теку %s: %v", instDir, err)
	}

	// Реєстр встановлених збірок + per-instance конфіг + стан синхронізації.
	a.buildsReg.Remove(packID)
	_ = a.cfg.DeleteInstanceConfig(packID)
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

	// Transient-прогрес прибираємо.
	a.dlTransientMu.Lock()
	delete(a.dlTransient, packID)
	a.dlTransientMu.Unlock()

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

	// Запуск на іншому акаунті: стан файлів не змінюємо, але позначаємо
	// RunningOn, щоб sidebar показав, на якому акаунті збірка працює.
	if s := a.sessionsM.RunningOnBuild(e.ID); s != nil {
		if s.AccountID == accID {
			view.Status = builds.StatusRunning
		} else {
			view.RunningOn = s.AccountName
		}
	}
	return view
}

// customBuildView будує View однієї кастомної (не-Shaurma) збірки для
// вказаного акаунта — дзеркалить buildView, але джерело даних не
// api.PackIndexEntry (сервер), а локальний custompack.Pack.
func (a *App) customBuildView(cp custompack.Pack, accID string) builds.View {
	build := builds.Build{
		ID:            cp.ID,
		Kind:          builds.KindCustom,
		Name:          cp.Name,
		Description:   cp.Description,
		MCVersion:     cp.MCVersion,
		Loader:        cp.Loader,
		LoaderVersion: cp.LoaderVersion,
		Icon:          cp.Icon,
		Color:         cp.Color,
		Source:        cp.Source,
	}
	if cp.IconPath != "" {
		// Локальна кастомна іконка: те саме "asset://" псевдо-джерело, яке
		// GetPackAsset розпізнає окремою гілкою (файл на диску, не CDN URL).
		build.IconURL = "local-file://" + cp.IconPath
	}
	if cp.BackgroundPath != "" {
		build.BackgroundURL = "local-file://" + cp.BackgroundPath
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
	} else if !installed {
		// Кастомна збірка щойно створена вручну (не імпортована) — файлів
		// гри ще немає, це нормальний стан "не встановлено", а не помилка.
		status = builds.StatusNotInstalled
	} else {
		status = builds.ComputeFileStatus(installed, entry.InstalledVersion, "")
	}
	a.dlTransientMu.Unlock()

	view := builds.View{
		Build:     build,
		Status:    status,
		Installed: installed,
		Progress:  progress,
	}

	if s := a.sessionsM.RunningOnBuild(cp.ID); s != nil {
		if s.AccountID == accID {
			view.Status = builds.StatusRunning
		} else {
			view.RunningOn = s.AccountName
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
