// Package install переносить завантажені компоненти з тимчасової теки
// в installDir, замінюючи попередні версії (backend.dll, frontend.zip...).
//
// Головна складність саме тут, не в завантаженні: на Windows не можна
// перезаписати файл запущеного процесу (ERROR_SHARING_VIOLATION), а файл
// теоретично міг щойно звільнитись, але процес ОС ще не встиг віддати
// хендл (антивірус тримає файл на скані, і т.п.). Тому Apply:
//  1. Чекає (з таймаутом), поки файл стане замінюваним — не покладається
//     на "процес вже мертвий", а пробує реальну операцію і повторює при
//     невдачі.
//  2. Компоненти, що належать ПОТОЧНОМУ процесу (завантажена updater.dll
//     чи виконуваний shell.exe — IsLockedComponent у manifest), взагалі не
//     замінює на місці: кладе поруч як <name>.new, а довстановлення
//     виконує shell при наступному старті (applyPendingUpdates).
//  3. Замінює через "перейменувати старий убік → перемістити новий на
//     його місце", а не прямий перезапис — це атомарна операція на
//     NTFS (rename в межах одного диска), тому навіть якщо процес
//     впаде рівно між цими двома кроками, ми ніколи не лишаємось БЕЗ
//     робочого файлу (старий лежить під .previous, і Rollback може
//     повернути його).
package install

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/manifest"
)

const (
	replaceRetryInterval = 300 * time.Millisecond
	replaceMaxWait       = 15 * time.Second
)

// BackupSuffix — куди відкладається попередня версія файлу перед тим, як
// на її місце стає нова. Лишається на диску до наступного успішного
// оновлення (перезаписується) — це навмисний, дешевий rollback-механізм:
// якщо новий launcher.exe виявиться битий (не запускається), користувач
// (або підтримка) може вручну перейменувати .previous назад.
// Експортовано: core.cleanupOtherEditionExe використовує той самий
// суфікс, щоб прибрати артефакти заміни .exe при перемиканні редакції.
const BackupSuffix = ".previous"

const backupSuffix = BackupSuffix

// PendingSuffix — суфікс тимчасового файлу для нової версії компонента,
// яку сам апдейтер довстановити не може (завантажений updater.dll/.so чи
// запущений виконуваний файл — див. manifest.IsLockedComponent /
// manifest.UpdaterLibraryName). Shell при наступному старті бачить
// <name>.new і перейменовує на місце; інсталятор також довстановлює
// залишені .new. Експортовано з тієї самої причини, що BackupSuffix.
const PendingSuffix = ".new"

const pendingSuffix = PendingSuffix

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
		// БЕЗПЕКА: повторна перевірка sha256/розміру безпосередньо перед
		// застосуванням — між завантаженням і заміною файл лежить на диску,
		// і його могли підмінити (інший процес користувача, шкідливе ПЗ).
		if err := download.VerifyFile(dc.Component, dc.TempPath); err != nil {
			if lang == "en" {
				return fmt.Errorf("integrity check failed for %s: %w", dc.Path, err)
			}
			return fmt.Errorf("перевірка цілісності %s: %w", dc.Path, err)
		}

		// updater.dll і поточний shell.exe — файли процесу, що ЗАРАЗ
		// виконує цей самий Apply (updater.dll завантажена через
		// LoadLibrary; shell.exe — сам exe). Windows не дає їх
		// перезаписати/перейменувати. Тому не намагаємось замінити їх на
		// місці — кладемо поруч як <name>.new, а довстановлення
		// (видалення старого, перейменування .new) виконає shell при
		// своєму наступному старті (applyPendingUpdates) або інсталятор.
		if manifest.IsLockedComponent(dc.Path) {
			pendingPath := filepath.Join(installDir, filepath.Base(dc.Path)+pendingSuffix)
			if onStatus != nil {
				if lang == "en" {
					onStatus("Preparing component update...")
				} else {
					onStatus("Підготовка оновлення компонента...")
				}
			}
			// Прибираємо застарілий .new з минулого разу (якщо shell
			// чомусь не встиг його довстановити) — інакше Rename нижче
			// впаде на Windows (rename поверх існуючого файлу заборонено).
			os.Remove(pendingPath)
			err := os.Rename(dc.TempPath, pendingPath)
			if err != nil {
				// Антивірус може тримати свіжий файл без FILE_SHARE_DELETE —
				// читання дозволене, тож копіюємо замість переміщення.
				err = copyFile(dc.TempPath, pendingPath)
			}
			if err != nil {
				if lang == "en" {
					return fmt.Errorf("failed to stage component update: %w", err)
				}
				return fmt.Errorf("не вдалось підготувати оновлення компонента: %w", err)
			}
			applied = append(applied, dc.Component)
			continue
		}

		// kind=archive: zip-архів (frontend.zip) розпаковується в
		// extractTo (installDir/frontend/). Сам архів на місце компонента
		// НЕ кладемо: після розпаковки він не потрібен (Diff порівнює
		// маніфести, а не файли на диску), а rename щойно скачаного файлу
		// з TEMP — типова точка відмови: антивірус тримає новий файл на
		// скані хендлом БЕЗ FILE_SHARE_DELETE, тож Windows забороняє
		// перейменування (реальний інцидент: "file still locked after 15s
		// wait: rename C:\...\Temp\shrm-update\frontend.zip"). Розпаковка
		// лише ЧИТАЄ файл — це дозволено навіть під час сканування.
		if dc.Kind == "archive" {
			if onStatus != nil {
				if lang == "en" {
					onStatus(fmt.Sprintf("Installing %s...", dc.Path))
				} else {
					onStatus(fmt.Sprintf("Встановлення %s...", dc.Path))
				}
			}
			if err := extractArchive(dc.TempPath, filepath.Join(installDir, filepath.FromSlash(dc.ExtractTo))); err != nil {
				if lang == "en" {
					return fmt.Errorf("failed to extract %s: %w", dc.Path, err)
				}
				return fmt.Errorf("не вдалось розпакувати %s: %w", dc.Path, err)
			}
			// Прибираємо тимчасовий архів і застарілий zip попередніх версій
			// (якщо лишився на диску) — installDir не смітиться багато-
			// мегабайтними frontend.zip після кожного оновлення.
			zipPath := filepath.Join(installDir, filepath.FromSlash(dc.Path))
			_ = os.Remove(zipPath)
			_ = os.Remove(zipPath + backupSuffix)
			_ = os.Remove(dc.TempPath)
			applied = append(applied, dc.Component)
			continue
		}

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
	// просто перемістити, нема що заміняти. Якщо rename впав (антивірус
	// ще тримає новий файл на скані без FILE_SHARE_DELETE) — копіюємо:
	// читання під час сканування зазвичай дозволене.
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		if err := os.Rename(srcPath, destPath); err == nil {
			return nil
		}
		return copyFile(srcPath, destPath)
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
			lastErr = err
			// Rename не вдався — найчастіше антивірус тримає щойно скачаний
			// файл на скані: хендл відкритий БЕЗ FILE_SHARE_DELETE, тож
			// Windows забороняє перейменування (йому потрібен DELETE-доступ
			// до джерела). Читання при цьому дозволене — копіюємо байти
			// замість переміщення; старий файл лишається у .previous (той
			// самий механізм відкату). Якщо і копія не йде (скан ще триває)
			// — відкочуємо старий назад і пробуємо ще раз.
			_ = os.Rename(backupPath, destPath)
			if cerr := copyFile(srcPath, destPath); cerr == nil {
				return nil
			}
			time.Sleep(replaceRetryInterval)
			continue
		}

		return nil
	}

	return fmt.Errorf("file still locked after %s wait: %w", replaceMaxWait, lastErr)
}

// copyFile копіює srcPath у destPath. Fallback для випадку, коли rename
// падає через AV-скан нового файлу (хендл сканування відкритий без
// FILE_SHARE_DELETE — Windows блокує перейменування, але дозволяє читання).
// Копія НЕ атомарна, але це лише запасний шлях: основний — rename у
// replaceFile, а тут ми просто забезпечуємо, що оновлення не вмирає через
// кількасекундний скан антивіруса.
func copyFile(srcPath, destPath string) error {
	in, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	// Пишемо у тимчасовий файл і лише повністю записаний, синхронізований
	// файл переносимо на місце: обірваний запис не лишає напівфайл
	// під справжнім ім'ям (інакше це бита DLL/EXE, що не запуститься).
	tmpPath := destPath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	_ = os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// Ліміти розпаковки (захист від zip-бомби): frontend.zip — це кілька
// десятків МБ статики, тож з великим запасом.
const (
	maxArchiveFiles      = 20000
	maxArchiveFileBytes  = 256 << 20  // 256 МБ на файл
	maxArchiveTotalBytes = 1024 << 20 // 1 ГБ сумарно
)

// extractArchive розпаковує zip у destDir (перестворюючи теку), з
// захистом від zip-slip, символічних посилань і zip-бомб. Підміна теки —
// з відкатом: стара версія видаляється лише після того, як нова вже
// стала на місце.
func extractArchive(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	if len(r.File) > maxArchiveFiles {
		return fmt.Errorf("archive has too many files (%d)", len(r.File))
	}

	tmp := destDir + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	cleanup := func(e error) error { _ = os.RemoveAll(tmp); return e }

	var total int64
	for _, f := range r.File {
		// zip-slip: ім'я не може бути абсолютним, містити диск/«..».
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" || strings.Contains(f.Name, ":") {
			return cleanup(fmt.Errorf("unsafe path in archive: %s", f.Name))
		}
		// Символічні посилання та інші спецфайли не розпаковуємо взагалі.
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !f.FileInfo().IsDir()) {
			return cleanup(fmt.Errorf("unsupported entry type in archive: %s", f.Name))
		}
		target := filepath.Join(tmp, rel)

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return cleanup(err)
			}
			continue
		}
		if f.UncompressedSize64 > maxArchiveFileBytes {
			return cleanup(fmt.Errorf("archive entry too large: %s", f.Name))
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return cleanup(err)
		}
		in, err := f.Open()
		if err != nil {
			return cleanup(err)
		}
		// O_EXCL: дублікат імені в архіві не перезапише вже записаний файл.
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			in.Close()
			return cleanup(err)
		}
		// Фактичний розмір обмежуємо самі — заголовок zip можна збрехати.
		n, cerr := io.Copy(out, io.LimitReader(in, maxArchiveFileBytes+1))
		if serr := out.Sync(); cerr == nil {
			cerr = serr
		}
		if xerr := out.Close(); cerr == nil {
			cerr = xerr
		}
		in.Close()
		if cerr != nil {
			return cleanup(cerr)
		}
		total += n
		if n > maxArchiveFileBytes || total > maxArchiveTotalBytes {
			return cleanup(fmt.Errorf("archive exceeds size limit"))
		}
	}

	// Підміна: стару теку відкладаємо вбік (з ретраями — антивірус/Explorer
	// можуть тримати файли), нову ставимо на місце; якщо не вдалось —
	// повертаємо стару назад.
	old := destDir + ".old"
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(destDir); err == nil {
		hadOld = true
		var rerr error
		for i := 0; i < 20; i++ {
			if rerr = os.Rename(destDir, old); rerr == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if rerr != nil {
			return cleanup(rerr)
		}
	}
	if err := os.Rename(tmp, destDir); err != nil {
		if hadOld {
			_ = os.Rename(old, destDir)
		}
		return cleanup(err)
	}
	if hadOld {
		_ = os.RemoveAll(old)
	}
	return nil
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
