package main

// health_check.go — Pack Doctor: агрегатор перевірок здоров'я збірки.
//
// Збирає результати вже існуючих перевірок (Java, диск, дублікати модів,
// RAM) в один HealthCheckReport. Новий код — лише агрегація; самі
// перевірки виконуються методами, що вже існують в інших файлах.

import (
	"fmt"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/java"
	"shaurma-launcher-backend/internal/minecraft"
	"shaurma-launcher-backend/internal/servers"
)

// ── Типи ────────────────────────────────────────────────────────────────

// HealthCheckSeverity — рівень серйозності пункту перевірки.
type HealthCheckSeverity string

const (
	HealthOK      HealthCheckSeverity = "ok"
	HealthWarning HealthCheckSeverity = "warning"
	HealthError   HealthCheckSeverity = "error"
)

// HealthCheckItem — один пункт звіту: що перевіряли, що знайшли, чи можна
// виправити автоматично.
type HealthCheckItem struct {
	ID          string              `json:"id"`
	Severity    HealthCheckSeverity `json:"severity"`
	Title       string              `json:"title"`
	Detail      string              `json:"detail,omitempty"`
	Fixable     bool                `json:"fixable"`
	FixActionID string              `json:"fixActionId,omitempty"`
}

// HealthCheckReport — повний звіт по збірці.
type HealthCheckReport struct {
	PackID  string              `json:"packId"`
	Items   []HealthCheckItem   `json:"items"`
	Overall HealthCheckSeverity `json:"overall"`
}

// ── Головна точка входу ──────────────────────────────────────────────────

// RunPackHealthCheck виконує всі перевірки здоров'я збірки і повертає
// зведений звіт. Викликається з фронту: (1) перед запуском гри, якщо
// overall != ok; (2) вручну через кнопку «Перевірити збірку».
func (a *App) RunPackHealthCheck(packID string) HealthCheckReport {
	items := []HealthCheckItem{}

	// ── 1. Java — свідомо НЕ перевіряємо ─────────────────────────────────
	//
	// Раніше тут був блок «Java <major> не встановлена» з Severity=error,
	// який змушував overall != 'ok' і показував модалку Pack Doctor ПЕРЕД
	// запуском. Але це марно: LaunchInstance і так викликає
	// ensureJavaForLaunch(), який САМ ставить потрібний мажор Java
	// (з version.json / RecommendedMajor) з живим прогресом
	// («Завантаження Java 21… 45%»). Тобто healthcheck дублював те, що
	// лаунчер і так зробить через секунду, і при цьому БЛОКУВАВ запуск
	// модалкою заради проблеми, яка вирішується автоматично без участі
	// користувача. Прибрано: Java залишилась на LaunchInstance
	// (там їй і місце — з прогресом і логікою fallback на вбудовану).
	// Пункт диска/дублікатів/RAM нижче — реальні рішення користувача,
	// їх і лишаємо.

	// ── 2. Вільне місце на диску (поріг: 2 ГБ = 2048 МБ) ───────────────
	storage := a.GetStorageUsage()
	const diskThresholdMB = 2048
	if storage.FreeDiskMB < diskThresholdMB {
		severity := HealthWarning
		if storage.FreeDiskMB < 512 {
			severity = HealthError
		}
		items = append(items, HealthCheckItem{
			ID:          "disk",
			Severity:    severity,
			Title:       fmt.Sprintf("Мало вільного місця на диску (%d МБ)", storage.FreeDiskMB),
			Detail:      "Рекомендується мінімум 2 ГБ вільного місця для стабільної роботи.",
			Fixable:     true,
			FixActionID: "cleanup_storage",
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:       "disk",
			Severity: HealthOK,
			Title:    fmt.Sprintf("Місця на диску достатньо (%d МБ вільно)", storage.FreeDiskMB),
		})
	}

	// ── 3. Дублікати модів ───────────────────────────────────────────────
	issues := a.CheckPreLaunchModIssues(packID)
	if len(issues.Duplicates) > 0 {
		names := make([]string, 0, len(issues.Duplicates))
		for _, g := range issues.Duplicates {
			for i, m := range g.Mods {
				if i == 0 {
					names = append(names, m.Name)
				}
			}
		}
		items = append(items, HealthCheckItem{
			ID:          "duplicates",
			Severity:    HealthWarning,
			Title:       fmt.Sprintf("Знайдено %d дублікат(и) модів", len(issues.Duplicates)),
			Detail:      strings.Join(names, ", "),
			Fixable:     true,
			FixActionID: "remove_duplicate_mods",
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:       "duplicates",
			Severity: HealthOK,
			Title:    "Дублікатів модів не знайдено",
		})
	}

	// ── 4. RAM: рекомендована vs ефективна ──────────────────────────────
	w := a.GetLaunchWarnings(packID)
	if w.RecommendedRAMMB > 0 && w.EffectiveRAMMB > 0 && w.EffectiveRAMMB < w.RecommendedRAMMB {
		items = append(items, HealthCheckItem{
			ID:       "ram",
			Severity: HealthWarning,
			Title:    "RAM менша за рекомендовану для збірки",
			Detail: fmt.Sprintf("Виділено %d МБ, рекомендовано %d МБ.",
				w.EffectiveRAMMB, w.RecommendedRAMMB),
			Fixable: false,
		})
	} else if w.AvailableRAMMB > 0 && int64(w.EffectiveRAMMB) > w.AvailableRAMMB {
		items = append(items, HealthCheckItem{
			ID:       "ram",
			Severity: HealthError,
			Title:    "Виділено більше RAM, ніж вільно в системі",
			Detail: fmt.Sprintf("Виділено %d МБ, вільно зараз %d МБ — можливий краш.",
				w.EffectiveRAMMB, w.AvailableRAMMB),
			Fixable: false,
		})
	} else {
		items = append(items, HealthCheckItem{
			ID:       "ram",
			Severity: HealthOK,
			Title:    fmt.Sprintf("RAM в нормі (%d МБ)", w.EffectiveRAMMB),
		})
	}

	return HealthCheckReport{
		PackID:  packID,
		Items:   items,
		Overall: worstSeverity(items),
	}
}

// packJavaMajor повертає потрібний мажор Java для збірки — той самий, що
// буде використаний при реальному запуску гри. Логіка ідентична AnalyzeUnusedJava:
// спочатку читаємо version.json профілю (loader → vanilla), потім fallback
// на RecommendedMajor за номером MC-версії.
func (a *App) packJavaMajor(packID string) int {
	builds := a.GetBuilds()
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	for _, v := range builds {
		if v.Build.ID != packID {
			continue
		}
		// 1. version.json loader-профілю (Forge/Fabric/etc.)
		if j, err := loadVersionJSON(mcBase, minecraft.LoaderVersionID(v.Build.Loader, v.Build.MCVersion, v.Build.LoaderVersion)); err == nil {
			if m := minecraft.EffectiveJavaMajor(j, mcBase); m > 0 {
				return m
			}
		}
		// 2. version.json vanilla
		if j, err := loadVersionJSON(mcBase, v.Build.MCVersion); err == nil {
			if m := minecraft.EffectiveJavaMajor(j, mcBase); m > 0 {
				return m
			}
		}
		// 3. Евристика за номером MC-версії
		return java.RecommendedMajor(v.Build.MCVersion)
	}
	// Збірку не знайдено — повертаємо типовий мажор
	return 21
}

// ── Автофікс ─────────────────────────────────────────────────────────────

// RunHealthCheckFix виконує один автоматичний виправник (fix action).
// Після успіху фронт повторно викликає RunPackHealthCheck для оновлення списку.
func (a *App) RunHealthCheckFix(packID, actionID string) error {
	switch actionID {
	case "cleanup_storage":
		a.CleanupOrphanLibraries()
		a.CleanupOrphanAssets()
		return nil

	case "remove_duplicate_mods":
		issues := a.CheckPreLaunchModIssues(packID)
		for _, g := range issues.Duplicates {
			keepIdx := 0
			for i, m := range g.Mods {
				if m.Enabled {
					keepIdx = i
					break
				}
			}
			for i, m := range g.Mods {
				if i == keepIdx {
					continue
				}
				if err := a.DeleteMod(packID, m.FileName); err != nil {
					return fmt.Errorf("не вдалося видалити %s: %w", m.FileName, err)
				}
			}
		}
		return nil

	case "install_java":
		// EnsureMajor встановлює саме той мажор, що потрібен збірці.
		major := a.packJavaMajor(packID)
		_, err := a.javaInst.EnsureMajor(major)
		servers.InvalidateJavaPathCache()
		return err

	default:
		return fmt.Errorf("невідомий actionID: %s", actionID)
	}
}

// ── Хелпери ──────────────────────────────────────────────────────────────

func worstSeverity(items []HealthCheckItem) HealthCheckSeverity {
	result := HealthOK
	for _, it := range items {
		if it.Severity == HealthError {
			return HealthError
		}
		if it.Severity == HealthWarning {
			result = HealthWarning
		}
	}
	return result
}
