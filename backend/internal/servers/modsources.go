// modsources.go — «єдине джерело» модів/плагінів для сервера.
//
// Дзеркало логіки збірок (internal/modsource): джерело — окремий .jar з
// довільного місця або ціла тека модів; перед запуском сервера лаунчер
// звіряє хеші й копіює змінене у mods/ (або plugins/). Конфіг лежить у
// <serverDir>/.shaurma/, не в самих mods/plugins.
package servers

import (
	"fmt"
	"path/filepath"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/modsource"
)

// contentName нормалізує "mods" | "plugins" (усе інше — помилка).
func contentName(name string) (string, error) {
	switch name {
	case modsource.NameMods, modsource.NamePlugins:
		return name, nil
	}
	return "", fmt.Errorf("невідомий тип контенту: %q (очікували mods|plugins)", name)
}

func sourceStoreFor(srv *Server, name string) modsource.Store {
	return modsource.Store{Root: srv.Dir, Name: name}
}

func contentDirOf(srv *Server, name string) string {
	return filepath.Join(srv.Dir, name)
}

// serverContentLockedErr — чи заборонена ручна зміна fileName у mods/plugins
// сервера джерелом. fileName=="" — дії без конкретного файлу.
func serverContentLockedErr(srv *Server, name, fileName string) error {
	return sourceStoreFor(srv, name).LockedErr(fileName)
}

// GetServerModSources повертає прикріплені джерела сервера (name: mods|plugins).
func (a *API) GetServerModSources(serverID, name string) (modsource.View, error) {
	name, err := contentName(name)
	if err != nil {
		return modsource.View{}, err
	}
	srv, err := a.store.Get(serverID)
	if err != nil {
		return modsource.View{}, err
	}
	return sourceStoreFor(srv, name).View()
}

// AddServerModSource прикріплює джерело (kind: file|folder) і одразу
// синхронізує файли, якщо сервер зупинений.
func (a *API) AddServerModSource(serverID, name, kind, path string) (modsource.View, error) {
	name, err := contentName(name)
	if err != nil {
		return modsource.View{}, err
	}
	srv, err := a.store.Get(serverID)
	if err != nil {
		return modsource.View{}, err
	}
	if a.manager.IsRunning(serverID) {
		return modsource.View{}, fmt.Errorf("зупиніть сервер, щоб змінити джерела")
	}
	st := sourceStoreFor(srv, name)
	if _, err := st.Add(kind, path, contentDirOf(srv, name)); err != nil {
		return modsource.View{}, err
	}
	res, err := st.Sync(contentDirOf(srv, name), func(p modsource.Progress) {
		a.notifySourceProgress(srv.ID, p)
	})
	if err != nil {
		return modsource.View{}, err
	}
	if len(res.Errors) > 0 {
		return modsource.View{}, fmt.Errorf("не вдалося скопіювати: %s", res.Errors[0])
	}
	return st.View()
}

// RemoveServerModSource відкріплює джерело; вже скопійовані файли лишаються.
func (a *API) RemoveServerModSource(serverID, name, sourceID string) (modsource.View, error) {
	name, err := contentName(name)
	if err != nil {
		return modsource.View{}, err
	}
	srv, err := a.store.Get(serverID)
	if err != nil {
		return modsource.View{}, err
	}
	st := sourceStoreFor(srv, name)
	if err := st.Remove(sourceID); err != nil {
		return modsource.View{}, err
	}
	return st.View()
}

// ServerContentLocked — для браузера контенту: чи можна ставити новий
// контент у mods/plugins цього сервера (false, якщо там тека-джерело).
func (a *API) ServerContentLocked(serverID, name string) error {
	name, err := contentName(name)
	if err != nil {
		return err
	}
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	return serverContentLockedErr(srv, name, "")
}

func (a *API) notifySourceProgress(serverID string, p modsource.Progress) {
	msg := "Оновлення модів із джерел…"
	if p.File != "" {
		msg = fmt.Sprintf("Оновлення модів із джерел: %s (%d/%d)", p.File, p.Done, p.Total)
	}
	a.manager.NotifyInstallProgress(serverID, ServerInstallProgress{
		Stage:   "mod_sources",
		Percent: p.Percent,
		Message: msg,
	})
}

// SyncServerSources синхронізує джерела mods/ і plugins/ сервера ПЕРЕД
// стартом. Помилки не блокують запуск: сервер стартує з тим, що є, а
// причина йде в лог/повідомлення (відсутнє джерело — просто попередження).
func (a *API) SyncServerSources(srv *Server) []string {
	var warnings []string
	for _, name := range []string{modsource.NameMods, modsource.NamePlugins} {
		st := sourceStoreFor(srv, name)
		cfg, err := st.Load()
		if err != nil || len(cfg.Sources) == 0 {
			continue
		}
		res, err := st.Sync(contentDirOf(srv, name), func(p modsource.Progress) {
			a.notifySourceProgress(srv.ID, p)
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		for _, m := range res.Missing {
			warnings = append(warnings, fmt.Sprintf("джерело не знайдено: %s — ви можете його відкріпити", m.Path))
		}
		for _, e := range res.Errors {
			warnings = append(warnings, e)
		}
	}
	return warnings
}

// annotateModEntries позначає у списку модів ті, що походять з джерела.
func annotateModEntries(st modsource.Store, mods []model.ModEntry) {
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

// annotatePluginInfos — те саме для плагінів (ServerModInfo).
func annotatePluginInfos(st modsource.Store, plugins []ServerPluginInfo) {
	cfg, err := st.Load()
	if err != nil || len(cfg.Sources) == 0 {
		return
	}
	for i := range plugins {
		if an := st.Annotate(cfg, plugins[i].FileName); an != nil {
			plugins[i].SourceKind = an.Kind
			plugins[i].SourcePath = an.Path
			plugins[i].SourceMissing = an.Missing
		}
	}
}
