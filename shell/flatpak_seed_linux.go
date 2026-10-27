//go:build linux

package main

// flatpak_seed_linux.go — "насіннєве" копіювання компонентів для
// Flatpak-збірки. Той самий bootstrap-контракт, що bootstrap.go
// (backend.so/updater.so/frontend/index.html мають опинитись у
// exeDir(), інакше launcherIncomplete()==true), але БЕЗ мережі: файли
// вже лежать у /app (read-only частина Flatpak-образу, запечена під час
// flatpak-builder), тут вони лише КОПІЮЮТЬСЯ у writable
// $XDG_DATA_HOME/ShaurmLauncher (Flatpak сам мапить цей шлях на
// ~/.var/app/<app-id>/data — жодного --filesystem= додатково не треба).
//
// НАВІЩО ЦЕ ІСНУЄ: Flathub-рев'ю відмовляє застосункам, які після
// встановлення докачують виконуваний код зі стороннього CDN (чиста
// bootstrap.go-архітектура — саме так). Запікши стартовий набір
// компонентів прямо в Flatpak-образ (com.shaurma.*.yml, install -Dm... у
// /app/shaurma-seed/), застосунок ПОВНІСТЮ самодостатній одразу після
// встановлення — мережа для першого запуску взагалі не потрібна.
// updater.so, вже завантажений зі свіжо-скопійованих файлів, і далі сам
// перевіряє й підтягує новіші версії компонентів з CDN — той самий
// механізм, що на Windows/AppImage/RPM/DEB, без жодних змін у ньому.
//
// БЕЗПЕКА ВІД ПОБІЧНИХ ЕФЕКТІВ: спрацьовує лише коли ОБИДВА:
//  1. процес реально запущено всередині Flatpak-пісочниці (надійний
//     маркер — сам Flatpak завжди створює /.flatpak-info у контейнері,
//     задокументовано в offical Flatpak sandbox docs);
//  2. launcherIncomplete()==true (щось із трійки backend.so/updater.so/
//     frontend/index.html відсутнє в exeDir()).
//
// На AppImage/DEB/RPM/Windows /.flatpak-info просто не існує — функція
// одразу повертає false, і решта bootstrap-логіки (мережева докачка з
// CDN через bootstrap.go) лишається ПОВНІСТЮ незмінною для цих шляхів
// доставки.

import (
	"io"
	"os"
	"path/filepath"
)

// flatpakSeedDir — тека всередині Flatpak-образу, куди
// com.shaurma.Launcher.yml/com.shaurma.LauncherBase.yml install-Dm-ять
// заздалегідь зібрані backend.so/updater.so/frontend/updater-frontend/ +
// launcher-version.json. /app — стандартний
// префікс Flatpak-застосунку (FLATPAK_DEST у manifest-командах), сюди
// нічого не пишеться під час роботи — лише читається один раз при seed.
const flatpakSeedDir = "/app/shaurma-seed"

// runningInsideFlatpak — /.flatpak-info присутній лише всередині
// Flatpak-пісочниці (створюється самим flatpak run, а не застосунком) —
// офіційно задокументований і рекомендований спосіб детекту, набагато
// надійніший за перевірку змінних середовища (FLATPAK_ID теж є, але не
// гарантовано для всіх версій flatpak/bwrap).
func runningInsideFlatpak() bool {
	_, err := os.Stat("/.flatpak-info")
	return err == nil
}

// seedFromFlatpakIfNeeded копіює запечений набір компонентів у exeDir(),
// якщо застосунок працює у Flatpak і чогось із набору бракує. Викликати
// ДО першої перевірки launcherIncomplete() у main.go (той самий момент,
// де зараз обирається loadUpdaterAndShowSplash() чи
// bootstrapAndLoadUpdater()) — після успішного seed launcherIncomplete()
// поверне false, і застосунок піде звичайним "все вже на диску" шляхом
// без жодної мережевої активності при першому запуску.
//
// САМЕ ТОМУ launcher-version.json теж входить у набір: updater порівнює
// версію/хеші саме з локальним маніфестом (manifest.Load). Без нього
// маніфест = порожній, Diff() вважає НОВИМИ всі компоненти і перший
// запуск качає з CDN ~120 МБ (backend.so, updater.so, frontend.zip,
// updater-frontend.zip), які вже лежать у /app. Маніфест копіюємо
// ОСТАННІМ, щоб "встановлено" не позначилось раніше за самі файли.
//
// Навмисно НЕ фатальна: якщо копіювання з якоїсь причини не вдалось
// (пошкоджений образ, брак диска в $XDG_DATA_HOME тощо) — просто
// повертаємось із launcherIncomplete()==true, і звичний
// bootstrapAndLoadUpdater() підхоплює цю ж роль через CDN, як і завжди.
// Це свідомий фолбек, а не помилка, яку треба показувати користувачу.
func seedFromFlatpakIfNeeded() {
	if !runningInsideFlatpak() {
		return
	}
	dir := exeDir()

	// Що саме бракує — рахуємо ДО копіювання. Кожен пункт незалежний:
	//   * launcherIncomplete() — тіло встановлення (backend.so/updater.so/
	//     frontend/index.html); шлях чистого першого запуску;
	//   * launcher-version.json — локальний маніфест (див. вище);
	//   * updater-frontend/ — UI самого апдейтера (splash). Його немає ні в
	//     launcherIncomplete(), ні в старому seed-наборі, тож апдейтер
	//     докачував його з CDN при кожному чистому встановленні.
	needCore := launcherIncomplete()
	needManifest := !fileExists(filepath.Join(dir, "launcher-version.json"))
	needUpdaterUI := !dirHasFiles(filepath.Join(dir, "updater-frontend"))
	if !needCore && !needManifest && !needUpdaterUI {
		return // все на місці (звичайний повторний запуск) — нема що копіювати
	}
	if _, err := os.Stat(flatpakSeedDir); err != nil {
		return // образ зібрано без /app/shaurma-seed (старий Flatpak-білд) — bootstrap.go підхопить
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		bootstrapLog(dir, "flatpak seed: не вдалось створити %s: %v", dir, err)
		return
	}

	if needCore {
		if err := copyFile(filepath.Join(flatpakSeedDir, backendLibraryName()), filepath.Join(dir, backendLibraryName())); err != nil {
			bootstrapLog(dir, "flatpak seed: %s: %v", backendLibraryName(), err)
			return
		}
		if err := copyFile(filepath.Join(flatpakSeedDir, updaterLibraryName()), filepath.Join(dir, updaterLibraryName())); err != nil {
			bootstrapLog(dir, "flatpak seed: %s: %v", updaterLibraryName(), err)
			return
		}
		if err := copyDirRecursive(filepath.Join(flatpakSeedDir, "frontend"), filepath.Join(dir, "frontend")); err != nil {
			bootstrapLog(dir, "flatpak seed: frontend/: %v", err)
			return
		}
	}

	if needUpdaterUI {
		if err := copyDirRecursive(filepath.Join(flatpakSeedDir, "updater-frontend"), filepath.Join(dir, "updater-frontend")); err != nil {
			bootstrapLog(dir, "flatpak seed: updater-frontend/: %v", err)
			return
		}
	}

	// Останнім — маніфест: після нього updater вважатиме набір встановленим
	// і не качатиме нічого, окрім справді новіших компонентів.
	if needManifest {
		if err := copyFile(filepath.Join(flatpakSeedDir, "launcher-version.json"), filepath.Join(dir, "launcher-version.json")); err != nil {
			bootstrapLog(dir, "flatpak seed: launcher-version.json: %v", err)
			return
		}
	}

	bootstrapLog(dir, "flatpak seed: скопійовано з %s у %s (core=%v manifest=%v updater-ui=%v)", flatpakSeedDir, dir, needCore, needManifest, needUpdaterUI)
}

// fileExists — чи існує шлях (файл або тека); помилки доступу трактуємо як
// "немає", бо seed — це best-effort фолбек, а не критичний шлях.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dirHasFiles — чи тека існує і не порожня (для перевірки архівних
// компонентів на кшталт updater-frontend/: сама тека може лишитись після
// обірваного оновлення, і тоді seed не повинен її пропускати).
func dirHasFiles(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

// copyFile — просте побайтове копіювання одного файлу (з правами 0644).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// copyDirRecursive копіює всю теку (розпакований frontend/ з index.html
// та рештою статики) з src у dst, зберігаючи структуру.
func copyDirRecursive(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}
