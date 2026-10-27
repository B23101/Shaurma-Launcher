package main

import (
	"fmt"
	"os"
	"path/filepath"

	"shaurma-launcher-backend/internal/java"
	"shaurma-launcher-backend/internal/minecraft"
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

// removeMinecraftVersionFiles видаляє версію зі СПІЛЬНОЇ бази Minecraft:
// versions/<id>.json (маніфест) і теки versions/<id>/ (клієнтський jar,
// лоадерні файли). Якщо версії й так немає — просто нічого не робить
// (відсутність не вважається помилкою полагодження).
func removeMinecraftVersionFiles(mcBase, versionID string) {
	if versionID == "" || mcBase == "" {
		return
	}
	versionsDir := filepath.Join(mcBase, "versions")
	os.RemoveAll(filepath.Join(versionsDir, versionID))
	os.Remove(filepath.Join(versionsDir, versionID+".json"))
}

// packMinecraftDir — тека .minecraft конкретної збірки (реальні файли гри:
// моди, конфіги, saves; спільні версії/бібліотеки Minecraft лежать окремо
// у <dataDir>/minecraft — інсталер на a.inst).
func (a *App) packMinecraftDir(id string) string {
	return filepath.Join(a.packDir(id, ""), ".minecraft")
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
		// «Полагодити» = ПОВНЕ перевстановлення компонентів запуску цієї
		// збірки: видаляємо встановлений профіль лоадера ("інсталер"), саму
		// версію Minecraft і вбудовану Java для цієї версії гри — EnsureLoader/
		// EnsureVersion нижче перекачають їх заново (клієнтський jar,
		// бібліотеки, асети). Так користувач може полагодити погано скачані/
		// пошкоджені компоненти. Світи, моди, скріншоти та налаштування збірки
		// НЕ чіпаємо — вони лежать в instance-теці (.minecraft), а не в
		// спільній базі Minecraft.
		mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
		a.emit("installer:log", fmt.Sprintf("Полагодження збірки \"%s\": перевстановлення Minecraft %s, лоадера %s та Java...", cp.Name, cp.MCVersion, cp.Loader))

		// 1) Профіль лоадера (напр. fabric-loader-0.16.5-1.20.1) — лише для
		//    цієї збірки.
		if cp.Loader != "" && string(cp.Loader) != "vanilla" {
			profileID := minecraft.LoaderVersionID(string(cp.Loader), cp.MCVersion, cp.LoaderVersion)
			removeMinecraftVersionFiles(mcBase, profileID)
		}
		// 2) Сама версія Minecraft (versions/<mc>/ + versions/<mc>.json).
		removeMinecraftVersionFiles(mcBase, cp.MCVersion)
		// 3) Вбудована Java, яку використовує ця версія гри (java-<major>).
		if major := java.RecommendedMajor(cp.MCVersion); major > 0 {
			javaDir := filepath.Join(a.cfg.JavaDir(), fmt.Sprintf("java-%d", major))
			if fi, err := os.Stat(javaDir); err == nil && fi.IsDir() {
				os.RemoveAll(javaDir)
				a.emit("installer:log", fmt.Sprintf("Java %d видалено — буде встановлена заново.", major))
			}
		}

		a.inst.ResetCancelInstall()
		// Перевстановлюємо одразу (а не чекаємо наступного запуску), щоб
		// «Полагодити» завершувалось уже готовим до запуску станом.
		launchVersion, err := a.inst.EnsureLoader(string(cp.Loader), cp.MCVersion, cp.LoaderVersion)
		if err != nil {
			return fmt.Errorf("не вдалося перевстановити лоадер %s: %w", cp.Loader, err)
		}
		if _, err := a.inst.EnsureVersion(launchVersion); err != nil {
			return fmt.Errorf("не вдалося перевстановити Minecraft %s: %w", cp.MCVersion, err)
		}
		a.emit("installer:log", "Збірку полагоджено: Minecraft, лоадер та Java перевстановлені.")
		return nil
	}

	// Shaurma-збірка: синк сам рахує дельту і докачує лише те, чого бракує.
	if a.packs != nil {
		a.packs.DownloadPack(id, a)
		return nil
	}
	return fmt.Errorf("немає провайдера завантажень")
}
