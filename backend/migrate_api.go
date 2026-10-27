package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/launchermigrate"
	"shaurma-launcher-backend/internal/store"
)

// ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ────────────
// Після входу користувачу показується вікно «мігрувати збірки з інших
// лаунчерів» (один раз — прапорець MigrationPromptShown у БД). Сканування
// шукає лаунчери ЛИШЕ у їхніх стандартних теках даних (жодного обходу
// дисків — антивірусна безпека, див. launchermigrate). Імпорт — чисте
// копіювання: файли (без кешів), налаштування, ID/назва/іконка; дані
// інших лаунчерів ніколи не видаляються. Походження збірки зберігається
// (Pack.MigratedFrom), при колізії ID додається «(2)».

// MigrateScan сканує інші лаунчери у їхніх стандартних теках даних.
// Повертає обидва лаунчери (Found=true/false) — фронтенд показує
// перемикачі лише для знайдених. Кожна збірка, що вже перенесена у цей
// лаунчер (у store є збірка з тим самим MigratedFrom{Kind, ID}), позначається
// AlreadyImported=true — модалка показує бейдж «Вже завантажено».
func (a *App) MigrateScan() launchermigrate.ScanResult {
	res := launchermigrate.Scan()
	a.markAlreadyImported(&res)
	return res
}

// markAlreadyImported позначає збірки, які вже перенесені у цей лаунчер:
// у store кастомних збірок шукаємо збірку з MigratedFrom.Kind == kind і
// MigratedFrom.ID == id (саме ці поля записує Import при міграції). Так
// користувач бачить «ця збірка вже в мене є» і вирішує, чи переносити
// ще раз (дублікат отримає суфікс «(2)»).
func (a *App) markAlreadyImported(res *launchermigrate.ScanResult) {
	if a.customPacks == nil {
		return
	}
	existing := a.customPacks.List()
	for li := range res.Launchers {
		l := &res.Launchers[li]
		for pi := range l.Packs {
			p := &l.Packs[pi]
			if p.AlreadyImported {
				continue
			}
			for _, e := range existing {
				if e.MigratedFrom != nil && e.MigratedFrom.Kind == string(p.Kind) && e.MigratedFrom.ID == p.ID {
					p.AlreadyImported = true
					break
				}
			}
		}
	}
}

// MigrateStatus — стан системи міграції для пост-логін вікна і віджета
// в Налаштуваннях. ShouldShow=true лише якщо: є акаунти, вікно ще не
// показувалось і в інших лаунчерах є хоч одна збірка, яку ще не переносили
// (порожня тека Prism/Modrinth без збірок — приводу для вікна немає).
type MigrateStatus struct {
	ShouldShow     bool `json:"shouldShow"`
	PromptShown    bool `json:"promptShown"`
	HasAccounts    bool `json:"hasAccounts"`
	FoundLaunchers int  `json:"foundLaunchers"`
	// PackCount — скільки збірок, ЩО ЩЕ НЕ перенесені, знайдено в усіх
	// лаунчерах разом (саме від нього залежить ShouldShow).
	PackCount int                        `json:"packCount"`
	Launchers []launchermigrate.Launcher `json:"launchers"`
}

// MigrateStatus повертає стан системи міграції (див. вище).
func (a *App) MigrateStatus() MigrateStatus {
	s := a.cfg.GetSettings()
	scan := launchermigrate.Scan()
	a.markAlreadyImported(&scan)
	st := MigrateStatus{
		PromptShown:    s.MigrationPromptShown,
		FoundLaunchers: scan.FoundLaunchers,
		HasAccounts:    len(a.cfg.GetAccounts()) > 0,
		Launchers:      []launchermigrate.Launcher{},
	}
	// Передаємо лише знайдені лаунчери зі списком збірок (для віджета).
	for _, l := range scan.Launchers {
		if l.Found {
			st.Launchers = append(st.Launchers, l)
			for _, p := range l.Packs {
				if !p.AlreadyImported {
					st.PackCount++
				}
			}
		}
	}
	st.ShouldShow = !s.MigrationPromptShown && st.PackCount > 0 && st.HasAccounts
	return st
}

// MigrateDismiss позначає, що вікно міграції вже показувалось (галочка
// «ми показали» у БД): після цього воно не спливає автоматично, але
// віджет у Налаштуваннях лишається доступним.
func (a *App) MigrateDismiss() error {
	s := a.cfg.GetSettings()
	if !s.MigrationPromptShown {
		s.MigrationPromptShown = true
		return a.cfg.UpdateSettings(s)
	}
	return nil
}

// MigrateImportRequest — вибір користувача у модалці: збірки одного
// лаунчера (вкладка), які треба перенести.
type MigrateImportRequest struct {
	Launcher string   `json:"launcher"` // "prism" | "modrinth"
	PackIDs  []string `json:"packIds"`  // ID збірок у СТАРОМУ лаунчері
}

// MigrateImportResult — підсумок імпорту вибраних збірок.
type MigrateImportResult struct {
	Imported []string          `json:"imported"` // нові packID у цьому лаунчері
	Failed   []string          `json:"failed"`   // packID (старі), що не вдались
	Errors   map[string]string `json:"errors"`
}

// MigrateImport переносить вибрані збірки з іншого лаунчера. Прогрес кожної
// збірки — подією migrate:progress (launchermigrate.ImportProgress); після
// завершення всіх — migrate:done. Виконується асинхронно на боці фронтенду
// (модалка показує прогрес), метод повертає підсумок.
func (a *App) MigrateImport(req MigrateImportRequest) (MigrateImportResult, error) {
	res := MigrateImportResult{Imported: []string{}, Failed: []string{}, Errors: map[string]string{}}
	if len(req.PackIDs) == 0 {
		return res, nil
	}
	// Свіже сканування: шляхи/налаштування збірок могли змінитись відтоді,
	// як користувач відкрив модалку (нові збірки, перенесені теки).
	scan := launchermigrate.Scan()
	var src *launchermigrate.Launcher
	for i := range scan.Launchers {
		if string(scan.Launchers[i].Kind) == req.Launcher {
			src = &scan.Launchers[i]
			break
		}
	}
	if src == nil || !src.Found {
		return res, fmt.Errorf("лаунчер %s не знайдено", req.Launcher)
	}

	byID := map[string]launchermigrate.Pack{}
	for _, p := range src.Packs {
		byID[p.ID] = p
	}

	settings := a.cfg.GetSettings()
	for _, packID := range req.PackIDs {
		pack, ok := byID[packID]
		if !ok {
			res.Failed = append(res.Failed, packID)
			res.Errors[packID] = "збірку не знайдено при повторному скануванні"
			continue
		}
		newPack, err := launchermigrate.Import(pack, launchermigrate.ImportOptions{
			Store:       a.customPacks,
			InstanceDir: settings.InstanceDir,
			Suggest:     a.migrateSuggestStyle,
			OnProgress: func(p launchermigrate.ImportProgress) {
				p.Launcher = req.Launcher
				a.emit("migrate:progress", p)
			},
		})
		if err != nil {
			res.Failed = append(res.Failed, packID)
			res.Errors[packID] = err.Error()
			continue
		}
		// Налаштування зі сканера → per-instance конфіг (RAM/Java-аргументи).
		a.applyMigratedConfig(newPack.ID, pack)
		// Збірка готова до запуску (файли вже скопійовані на місце).
		a.buildsReg.SetInstalled(newPack.ID, builds.KindCustom, "")
		a.emit("builds:changed", newPack.ID)
		res.Imported = append(res.Imported, newPack.ID)
	}

	// Вікно показувалось, користувач зробив вибір — більше не спливаємо.
	_ = a.MigrateDismiss()
	a.emit("migrate:done", res)
	return res, nil
}

// migrateSuggestStyle — призначення пресет-іконки+кольору збірці БЕЗ
// PNG-іконки через Groq (json-режим; валідація у ai.PackStyle). При
// недоступності ШІ повертає порожнє — launchermigrate застосовує
// детермінований фолбек за назвою.
func (a *App) migrateSuggestStyle(name, mcVersion, loader string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	style := a.groqClient().SuggestPackStyle(ctx, name, mcVersion, loader)
	return style.Icon, style.Color
}

// applyMigratedConfig переносить налаштування збірки (RAM/Java-аргументи)
// зі сканера іншого лаунчера у per-instance конфіг нової збірки.
func (a *App) applyMigratedConfig(newPackID string, src launchermigrate.Pack) {
	cfg := a.cfg.GetInstanceConfig(newPackID)
	changed := false
	if src.MaxRAMMB > 0 {
		cfg.MaxRAMOverride = src.MaxRAMMB
		cfg.MinRAMOverride = src.MinRAMMB
		changed = true
	}
	if src.JvmArgs != "" {
		cfg.JavaArgsOverride = src.JvmArgs
		changed = true
	}
	if changed {
		_ = a.cfg.SaveInstanceConfig(cfg)
	}
}

// MigrateIconDataURL читає PNG-іконку збірки іншого лаунчера (абсолютний
// шлях зі сканування) і повертає data URL для прев'ю в модалці міграції.
// WebView не має доступу до довільних шляхів ФС — зображення йдуть через
// бекенд. Порожній рядок при помилці (іконка могла зникнути/бути не PNG).
func (a *App) MigrateIconDataURL(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if a.wardrobe != nil {
		return a.wardrobe.TextureDataURL(path)
	}
	// Фолбек без wardrobe-сервісу (тести/dev): читаємо напряму.
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

// GetDBHealth повертає стан здоров'я бази даних (відновлення з резервної
// копії / навмисне скидання) — фронтенд показує модальне вікно з
// поясненням, щоб «не мовчати про причину втрати даних користувача».
func (a *App) GetDBHealth() store.DBHealth {
	if st := a.cfg.Store(); st != nil {
		return st.Health()
	}
	return store.DBHealth{}
}

// emitDBHealth шле подію db:health одразу після старту (ServiceStartup):
// фронтенд показує модальне вікно з поясненням, коли стан бази відхиляється
// від норми (відновлено з бекупу / стерто навмисно / працює в пам'яті).
func (a *App) emitDBHealth() {
	h := a.GetDBHealth()
	if h.RestoredFromBackup || h.ErasedIntentionally {
		a.emit("db:health", h)
	}
}
