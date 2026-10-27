package main

import (
	"fmt"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/modsource"
)

// ── «Єдине джерело» модів для збірок ───────────────────────────────────
// Розробник прикріплює до збірки окремий jar (або цілу теку модів іншої
// збірки), а лаунчер ПЕРЕД запуском гри та ПЕРЕД пакуванням у модпак
// звіряє хеші й копіює змінене у mods/. Логіка — у internal/modsource;
// тут лише прив'язка до збірок, прогрес для UI та захист від ручних змін.
// Конфіг лежить у <тека збірки>/.shaurma/mods-sources.json.
//
// Офіційні Shaurma-збірки керуються маніфестом worker'а (він сам
// синхронізує й видаляє моди), тому джерела для них не підтримуються.

func (a *App) packSourceStore(packID string) modsource.Store {
	return modsource.Store{Root: a.packDir(packID, ""), Name: modsource.NameMods}
}

// GetModSources повертає прикріплені джерела збірки.
func (a *App) GetModSources(packID string) (modsource.View, error) {
	if packID == "" {
		return modsource.View{Sources: []modsource.SourceView{}}, fmt.Errorf("не вказано packID")
	}
	return a.packSourceStore(packID).View()
}

// PickModSourceFile — діалог вибору одного .jar як джерела.
func (a *App) PickModSourceFile() (string, error) {
	p := bridgeDialogOpenFile("Оберіть jar-файл мода (джерело)", false, map[string][]string{
		"Mod files (*.jar)": {"jar"},
	})
	return p, nil
}

// PickModSourceFolder — діалог вибору теки модів як джерела.
func (a *App) PickModSourceFolder() (string, error) {
	p := bridgeDialogOpenFile("Оберіть теку модів (джерело)", true, nil)
	return p, nil
}

// AddModSource прикріплює джерело (kind: "file" | "folder") і одразу
// синхронізує mods/. Прогрес іде подією "mods:sources:progress".
func (a *App) AddModSource(packID, kind, path string) (modsource.View, error) {
	empty := modsource.View{Sources: []modsource.SourceView{}}
	build, err := a.resolveLaunchBuild(packID)
	if err != nil {
		return empty, err
	}
	if build.IsShaurma {
		return empty, fmt.Errorf("для офіційних Shaurma-збірок джерела модів недоступні")
	}
	if err := a.modsBusyErr(packID); err != nil {
		return empty, err
	}
	if kind == modsource.KindFolder {
		if err := a.checkFolderSourceNotMirror(packID, path); err != nil {
			return empty, err
		}
	}
	st := a.packSourceStore(packID)
	if _, err := st.Add(kind, path, a.instanceModsDir(packID)); err != nil {
		return empty, err
	}
	res := a.syncPackSources(packID, func(p modsource.Progress) {
		a.emit("mods:sources:progress", map[string]any{
			"packId": packID, "done": p.Done, "total": p.Total,
			"file": p.File, "action": p.Action, "percent": p.Percent,
		})
	})
	a.invalidateModUpdateCache(packID)
	if len(res.Errors) > 0 {
		return empty, fmt.Errorf("не вдалося скопіювати: %s", res.Errors[0])
	}
	return st.View()
}

// RemoveModSource відкріплює джерело. Уже скопійовані моди ЛИШАЮТЬСЯ у
// mods/ як звичайні (їх знову можна вимикати й видаляти).
func (a *App) RemoveModSource(packID, sourceID string) (modsource.View, error) {
	st := a.packSourceStore(packID)
	if err := st.Remove(sourceID); err != nil {
		return modsource.View{Sources: []modsource.SourceView{}}, err
	}
	a.invalidateModUpdateCache(packID)
	return st.View()
}

// SyncModSources — ручний «оновити зараз» з діалогу джерел.
func (a *App) SyncModSources(packID string) (modsource.Result, error) {
	if err := a.modsBusyErr(packID); err != nil {
		return modsource.Result{}, err
	}
	res := a.syncPackSources(packID, func(p modsource.Progress) {
		a.emit("mods:sources:progress", map[string]any{
			"packId": packID, "done": p.Done, "total": p.Total,
			"file": p.File, "action": p.Action, "percent": p.Percent,
		})
	})
	if res.Changed() {
		a.invalidateModUpdateCache(packID)
	}
	return res, nil
}

// syncPackSources — спільне ядро для запуску, експорту та ручної
// синхронізації. Для збірок без джерел (і для Shaurma-збірок) — no-op.
func (a *App) syncPackSources(packID string, progress func(modsource.Progress)) modsource.Result {
	res := modsource.Result{Missing: []modsource.Missing{}, Errors: []string{}}
	if b, err := a.resolveLaunchBuild(packID); err != nil || b.IsShaurma {
		return res
	}
	r, err := a.packSourceStore(packID).Sync(a.instanceModsDir(packID), progress)
	if err != nil {
		r.Errors = append(r.Errors, err.Error())
	}
	return r
}

// describeSourceIssues перетворює підсумок синхронізації на текст
// попередження для гравця (порожній рядок — усе гаразд).
func describeSourceIssues(res modsource.Result) string {
	msg := ""
	for _, m := range res.Missing {
		if m.Kind == modsource.KindFolder {
			msg += fmt.Sprintf("Тека-джерело модів не знайдена: %s\nЗбірка запуститься з тими модами, що вже є. Ви можете відкріпити джерело в менеджері модів.\n\n", m.Path)
		} else {
			msg += fmt.Sprintf("Файл-джерело не знайдено: %s\nВикористовується наявна копія в mods (якщо вона є). Ви можете відкріпити джерело в менеджері модів.\n\n", m.Path)
		}
	}
	for _, e := range res.Errors {
		msg += "Не вдалося оновити мод: " + e + "\n"
	}
	return msg
}

// syncSourcesForLaunch — крок запуску гри: візуальний прогрес (стадія
// mod_sources) + попередження, якщо джерело зникло. Не блокує запуск.
func (a *App) syncSourcesForLaunch(instanceID string) {
	st := a.packSourceStore(instanceID)
	if cfg, err := st.Load(); err != nil || len(cfg.Sources) == 0 {
		return
	}
	a.emitLaunchProgressPct(instanceID, LaunchStageModSources, "Перевірка модів із джерел…", 0)
	res := a.syncPackSources(instanceID, func(p modsource.Progress) {
		msg := "Перевірка модів із джерел…"
		switch {
		case p.Action == "copy" && p.File != "":
			msg = fmt.Sprintf("Оновлення мода: %s (%d/%d)", p.File, p.Done+1, p.Total)
		case p.Action == "remove" && p.File != "":
			msg = fmt.Sprintf("Видалення мода: %s (%d/%d)", p.File, p.Done+1, p.Total)
		case p.File != "":
			msg = fmt.Sprintf("Перевірка мода: %s (%d/%d)", p.File, p.Done, p.Total)
		}
		a.emitLaunchProgressPct(instanceID, LaunchStageModSources, msg, p.Percent)
	})
	if res.Changed() {
		a.invalidateModUpdateCache(instanceID)
	}
	if w := describeSourceIssues(res); w != "" {
		a.emitLaunchWarning(instanceID, w)
	}
}

// syncSourcesForExport — крок пакування у модпак (будь-який формат):
// ПЕРЕД хешуванням/пакуванням оновлюємо mods/ із джерел, щоб у модпак
// потрапила актуальна версія. Прогрес — export:progress зі stage mod_sources.
func (a *App) syncSourcesForExport(packID string) error {
	st := a.packSourceStore(packID)
	if cfg, err := st.Load(); err != nil || len(cfg.Sources) == 0 {
		return nil
	}
	res := a.syncPackSources(packID, func(p modsource.Progress) {
		a.emit("export:progress", map[string]any{
			"packId":   packID,
			"stage":    "mod_sources",
			"current":  p.Done,
			"total":    p.Total,
			"fileName": p.File,
			"percent":  p.Percent,
		})
	})
	if res.Changed() {
		a.invalidateModUpdateCache(packID)
	}
	if len(res.Errors) > 0 {
		return fmt.Errorf("не вдалося оновити моди з джерел перед пакуванням: %s", res.Errors[0])
	}
	// Зниклі джерела не зупиняють пакування — у модпак іде наявна копія.
	return nil
}

// checkFolderSourceNotMirror — тека-джерело не повинна сама бути
// дзеркалом іншого джерела (ланцюжки A→B→C дають застарілі моди, а цикли
// A→B→A — нескінченне перезаписування).
func (a *App) checkFolderSourceNotMirror(packID, path string) error {
	for id := range a.cfg.AllInstanceConfigs() {
		if id == packID {
			continue
		}
		if !modsource.SamePath(a.instanceModsDir(id), path) {
			continue
		}
		if f, err := a.packSourceStore(id).Load(); err == nil {
			if fs := f.FolderSource(); fs != nil {
				return fmt.Errorf("ця тека сама є дзеркалом іншого джерела (%s) — оберіть оригінальну теку", fs.Path)
			}
		}
	}
	return nil
}

// modsLockedErr — чи заборонена ручна зміна fileName у mods/ збірки
// джерелом ("" — дії без конкретного файлу: імпорт, встановлення нового).
func (a *App) modsLockedErr(packID, fileName string) error {
	return a.packSourceStore(packID).LockedErr(fileName)
}

// annotatePackMods позначає у списку модів ті, що походять з джерела.
func (a *App) annotatePackMods(packID string, mods []model.ModEntry) {
	st := a.packSourceStore(packID)
	cfg, err := st.Load()
	if err != nil || len(cfg.Sources) == 0 {
		return
	}
	for i := range mods {
		if an := st.Annotate(cfg, mods[i].FileName); an != nil {
			mods[i].SourceKind = an.Kind
			mods[i].SourcePath = an.Path
			mods[i].SourceMissing = an.Missing
		}
	}
}

// ── Сервери: тонкі обгортки над servers.API ────────────────────────────

func (a *App) GetServerModSources(serverID, name string) (modsource.View, error) {
	return a.srvAPI.GetServerModSources(serverID, name)
}

func (a *App) AddServerModSource(serverID, name, kind, path string) (modsource.View, error) {
	return a.srvAPI.AddServerModSource(serverID, name, kind, path)
}

func (a *App) RemoveServerModSource(serverID, name, sourceID string) (modsource.View, error) {
	return a.srvAPI.RemoveServerModSource(serverID, name, sourceID)
}
