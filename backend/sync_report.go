package main

import (
	"fmt"
	"strings"

	"shaurma-launcher-backend/internal/sync"
)

// emitSyncReport друкує в консоль встановлення (подія "installer:log")
// компактний підсумок того, що конкретна сесія sync.Runner реально
// змінила на диску: скільки файлів оновлено, скільки override-тек
// перезаписано, скільки, навпаки, СВІДОМО ПРОПУЩЕНО (правило
// on_missing/first_install_only — саме те, що стосується config/ і
// options.txt), і скільки осиротілих файлів видалено.
//
// Повний, докладний слід (кожен шлях окремим рядком) і так пишеться в
// logs/launcher.log (internal/launchlog, компонент "Sync") самим
// Runner-ом по ходу синку — сюди виводимо лише зведення, щоб консоль
// встановлення не захаращувалась сотнями рядків при великій збірці.
// Список пропущених on_missing-тек друкуємо повністю (їх завжди мало,
// і саме це найчастіше треба перевірити "на око").
func (a *App) emitSyncReport(report sync.SyncReport) {
	updated := len(report.UpdatedFiles) + len(report.UpdatedFolders)
	removed := len(report.RemovedFiles)
	skipped := len(report.SkippedFolders)

	if updated == 0 && removed == 0 && skipped == 0 {
		a.emit("installer:log", "Синхронізація: усі файли вже актуальні, змін немає.")
		return
	}

	var parts []string
	if len(report.UpdatedFiles) > 0 {
		parts = append(parts, fmt.Sprintf("файлів оновлено: %d", len(report.UpdatedFiles)))
	}
	if len(report.UpdatedFolders) > 0 {
		parts = append(parts, fmt.Sprintf("тек перезаписано: %d", len(report.UpdatedFolders)))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("видалено як зайве: %d", removed))
	}
	a.emit("installer:log", "Синхронізація: "+strings.Join(parts, ", ")+".")

	if skipped > 0 {
		a.emit("installer:log", fmt.Sprintf(
			"Не чіпаю (правило «Оновити при старті», вже на диску): %s",
			strings.Join(report.SkippedFolders, ", ")))
	}
}

// emitSkippedProtectedFiles — той самий принцип, що emitSyncReport, але
// для кастомних (Modrinth/CurseForge) збірок при «Оновити з джерела»:
// та підсистема (internal/custompack, extractOverrides) не використовує
// sync.Runner/sync.SyncReport узагалі (окремий, спрощений механізм без
// серверних SyncRules), тому список захищених шляхів тут — простий
// []string, отриманий напряму від custompack.UpdatePackFromSource.
func (a *App) emitSkippedProtectedFiles(skipped []string) {
	if len(skipped) == 0 {
		return
	}
	a.emit("installer:log", fmt.Sprintf(
		"Не чіпаю (файли гравця, вже на диску): %s",
		strings.Join(skipped, ", ")))
}
