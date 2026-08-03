// Package install переносить завантажені компоненти з тимчасової теки
// в installDir, замінюючи попередню версію launcher.exe.
//
// Головна складність саме тут, не в завантаженні: на Windows не можна
// перезаписати файл запущеного процесу (ERROR_SHARING_VIOLATION), а
// launcher.exe теоретично міг щойно завершитись, але процес ОС ще не
// встиг звільнити хендл (антивірус тримає файл на скані, і т.п.). Тому
// Apply:
//  1. Чекає (з таймаутом), поки launcher.exe стане замінюваним —
//     не покладається на "процес вже мертвий", а пробує реальну
//     операцію і повторює при невдачі.
//  2. Замінює через "перейменувати старий убік → перемістити новий на
//     його місце", а не прямий перезапис — це атомарна операція на
//     NTFS (rename в межах одного диска), тому навіть якщо процес
//     впаде рівно між цими двома кроками, ми ніколи не лишаємось БЕЗ
//     робочого launcher.exe (старий лежить під іншим ім'ям, і Rollback
//     може повернути його).
package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"shrm-updater/internal/download"
	"shrm-updater/internal/manifest"
)

const (
	replaceRetryInterval = 300 * time.Millisecond
	replaceMaxWait       = 15 * time.Second
)

// backupSuffix — куди відкладається попередня версія файлу перед тим,
// як на її місце стає нова. Лишається на диску до наступного успішного
// оновлення (перезаписується) — це навмисний, дешевий rollback-механізм:
// якщо новий launcher.exe виявиться биту (не запускається), користувач
// (або підтримка) може вручну перейменувати .previous назад.
const backupSuffix = ".previous"

// Apply переносить кожен DownloadedComponent з temp у installDir,
// записує новий launcher-version.json. Повертає помилку, якщо
// ЖОДНОГО файлу не вдалось замінити — часткове застосування (одні
// файли оновились, інші ні) вважається помилкою всього оновлення:
// краще лишити стару консистентну версію, ніж мікс старих/нових файлів.
//
// В поточному форматі компонент рівно один (launcher.exe), тому "все
// або нічого" тут просте: якщо єдиний Apply-крок не вдався — маніфест
// НЕ переписується, і наступний запуск updater побачить той самий diff
// і спробує ще раз.
func Apply(ctx context.Context, installDir string, downloaded []download.DownloadedComponent, remote *manifest.Manifest, lang string, onStatus func(string)) error {
	applied := make([]manifest.Component, 0, len(downloaded))

	for _, dc := range downloaded {
		destPath := filepath.Join(installDir, filepath.FromSlash(dc.Path))
		if onStatus != nil {
			if lang == "en" {
				onStatus(fmt.Sprintf("Installing %s...", dc.Path))
			} else {
				onStatus(fmt.Sprintf("Встановлення %s...", dc.Path))
			}
		}

		if err := replaceFile(ctx, dc.TempPath, destPath); err != nil {
			if lang == "en" {
				return fmt.Errorf("failed to install %s: %w", dc.Path, err)
			}
			return fmt.Errorf("не вдалось встановити %s: %w", dc.Path, err)
		}
		applied = append(applied, dc.Component)
	}

	// Локальний манiфест = повний список компонентів remote (не лише
	// applied) — якщо колись компонентів стане більше одного і частина
	// з них не змінилась цього разу, локальний манiфест все одно має
	// описувати ПОВНУ поточну поставку, інакше наступний Diff() побачить
	// "відсутній" компонент і перекачає те, що насправді вже стоїть.
	newLocal := &manifest.Manifest{
		Version:      remote.Version,
		ReleaseNotes: remote.ReleaseNotes,
		Components:   remote.Components,
	}
	if err := manifest.Save(filepath.Join(installDir, "launcher-version.json"), newLocal); err != nil {
		if lang == "en" {
			return fmt.Errorf("writing launcher-version.json: %w", err)
		}
		return fmt.Errorf("запис launcher-version.json: %w", err)
	}

	_ = applied
	return nil
}

// replaceFile замінює destPath на srcPath атомарно (наскільки дозволяє
// файлова система), з ретраями на випадок ERROR_SHARING_VIOLATION.
func replaceFile(ctx context.Context, srcPath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	// Файл ще не існує (перший запуск, немає launcher.exe взагалі) —
	// просто перемістити, нема що заміняти.
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		return os.Rename(srcPath, destPath)
	}

	backupPath := destPath + backupSuffix

	deadline := time.Now().Add(replaceMaxWait)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Крок 1: прибрати попередній .previous, якщо лишився з
		// минулого разу (інакше наступний rename впаде на Windows,
		// де rename поверх існуючого файлу забороняється, на відміну
		// від POSIX).
		os.Remove(backupPath)

		// Крок 2: старий файл -> .previous (rename, не delete —
		// зберігаємо можливість відкату).
		if err := os.Rename(destPath, backupPath); err != nil {
			lastErr = err
			time.Sleep(replaceRetryInterval)
			continue
		}

		// Крок 3: новий файл на місце старого.
		if err := os.Rename(srcPath, destPath); err != nil {
			// Не вдалось поставити новий файл — відкочуємо старий
			// назад з .previous, щоб не лишити installDir без
			// launcher.exe взагалі.
			_ = os.Rename(backupPath, destPath)
			lastErr = err
			time.Sleep(replaceRetryInterval)
			continue
		}

		return nil
	}

	return fmt.Errorf("file still locked after %s wait: %w", replaceMaxWait, lastErr)
}

// Rollback повертає попередню версію компонента з .previous — викликач
// (UI) може запропонувати це користувачу, якщо новий launcher.exe не
// запускається після оновлення (напр. HealthCheck після старту провалився).
func Rollback(installDir, componentPath string) error {
	destPath := filepath.Join(installDir, filepath.FromSlash(componentPath))
	backupPath := destPath + backupSuffix
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("no saved previous version: %w", err)
	}
	tmp := destPath + ".rollback-tmp"
	if err := os.Rename(destPath, tmp); err != nil {
		return err
	}
	if err := os.Rename(backupPath, destPath); err != nil {
		_ = os.Rename(tmp, destPath) // відкат відкату — лишаємось на биту, але наявну версію
		return err
	}
	os.Remove(tmp)
	return nil
}
