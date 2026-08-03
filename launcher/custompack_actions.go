package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// ── Перевстановлення / полагодження збірок (кнопки на сторінці збірки) ──

// packKind визначає тип збірки за ID: "custom" — локальна кастомна
// (custompack.Store) чи "shaurma" — офіційна з каталогу Шаурма.
func (a *App) packKind(id string) (string, error) {
	if _, ok := a.customPacks.Get(id); ok {
		return "custom", nil
	}
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(id); err == nil && e != nil {
			return "shaurma", nil
		}
	}
	return "", fmt.Errorf("збірку не знайдено: %s", id)
}

// packMinecraftDir — тека .minecraft конкретної збірки (реальні файли гри:
// моди, конфіги, saves; спільні версії/бібліотеки Minecraft лежать окремо
// у <dataDir>/minecraft — інсталер на a.inst).
func (a *App) packMinecraftDir(id string) string {
	return filepath.Join(a.cfg.GetSettings().InstanceDir, id, ".minecraft")
}

// ReinstallPack — кнопка «Переставити заново». Видаляє ВСІ файли збірки
// (вміст теки .minecraft) і створює її заново, але ЗБЕРІГАЄ налаштування
// лаунчера: per-instance конфіг збірки (instance-configs.json), запис
// custom-packs.json та асети збірки (іконку/фон у корені теки збірки —
// поза .minecraft). Тека видаляється ЦІЛКОМ і створюється порожня, щоб
// залишки пошкоджених файлів не лишались.
//
//   - Shaurma-збірка: після очищення одразу запускається повторна качка
//     (синк з маніфестом, з нуля — стан синхронізації теж скидається).
//   - Кастомна збірка: файлів гри ще може не бути взагалі (вони
//     створюються при запуску Minecraft через спільний інсталер), тому
//     тут очищення означає "прибрати поточний стан" — наступний запуск
//     збере все заново.
func (a *App) ReinstallPack(id string) error {
	if id == "" {
		return nil
	}
	kind, err := a.packKind(id)
	if err != nil {
		return err
	}

	// Гру на цій збірці зупиняємо (на будь-якому акаунті), активну качку
	// скасовуємо — щоб рушій не писав у теку після її видалення.
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == id {
			_ = a.sessionsM.Stop(s.AccountID, s.BuildID)
		}
	}
	if a.syncMgr != nil {
		a.syncMgr.Cancel(id)
	}

	mcDir := a.packMinecraftDir(id)
	if err := os.RemoveAll(mcDir); err != nil {
		return fmt.Errorf("не вдалося очистити файли збірки: %w", err)
	}
	if err := os.MkdirAll(mcDir, 0755); err != nil {
		return fmt.Errorf("не вдалося створити .minecraft: %w", err)
	}

	if kind == "shaurma" {
		// Повне перевстановлення з нуля: скидаємо запис "встановлено" та
		// стан синхронізації, щоб дельта-калькуляція почала з пустого, і
		// одразу запускаємо качку (файли підуть заново).
		a.buildsReg.Remove(id)
		if st := a.syncStateStore(); st != nil {
			_ = st.Delete(id)
		}
		a.dlTransientMu.Lock()
		delete(a.dlTransient, id)
		a.dlTransientMu.Unlock()
		if a.packs != nil {
			a.packs.DownloadPack(id, a)
		}
	}

	a.emit("builds:changed", id)
	return nil
}

// RepairPack — кнопка «Полагодити». Перевстановлює залежності Minecraft і
// перевіряє файли збірки на пошкодження/відсутність.
//
//   - Кастомна збірка: a.inst.EnsureVersion перевіряє/докачує відсутні
//     компоненти версії (клієнтський jar, бібліотеки, індекс асетів) у
//     спільну базу <dataDir>/minecraft. Прогрес іде подіями installer:log.
//   - Shaurma-збірка: повторний синк з маніфестом перевіряє файли збірки
//     і докачує відсутні/пошкоджені (хеш-дельта), тобто "полагодження"
//     для офіційних збірок = примусовий апдейт-пересинк.
func (a *App) RepairPack(id string) error {
	kind, err := a.packKind(id)
	if err != nil {
		return err
	}

	if kind == "custom" {
		cp, _ := a.customPacks.Get(id)
		if err := os.MkdirAll(a.packMinecraftDir(id), 0755); err != nil {
			return err
		}
		a.emit("installer:log", fmt.Sprintf("Полагодження збірки \"%s\": перевірка Minecraft %s...", cp.Name, cp.MCVersion))
		if _, err := a.inst.EnsureVersion(cp.MCVersion); err != nil {
			return fmt.Errorf("не вдалося відновити Minecraft %s: %w", cp.MCVersion, err)
		}
		a.emit("installer:log", "Minecraft перевірено — відсутні/пошкоджені файли завантажено.")
		return nil
	}

	// Shaurma-збірка: синк сам рахує дельту і докачує лише те, чого бракує.
	if a.packs != nil {
		a.packs.DownloadPack(id, a)
		return nil
	}
	return fmt.Errorf("немає провайдера завантажень")
}
