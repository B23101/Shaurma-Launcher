package sync

import (
	"path/filepath"
	"strings"
	"time"
)

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

// Task — одне завдання завантаження в межах однієї з двох черг.
type Task struct {
	// Queue — до якої черги належить (QueueHashMods або QueueWorker).
	Queue QueueKind
	// Kind — тип файлу з маніфесту (для workerFiles); для hashMods — "".
	Kind WorkerFileKind
	// URL — джерело: зовнішній CDN для hashMods, Worker-шлях (уже з базовим
	// URL) для workerFiles.
	URL string
	// DestRelPath — шлях відносно кореня установки збірки (mods/xxx.jar,
	// options.txt, і т.д.). Для override-folder — це ТЕКА, куди
	// розпаковується завантажений архів (не сам .zip).
	DestRelPath string
	// IsArchive — true для override-folder: файл якається у тимчасове
	// місце і після завершення розпаковується як ціле, а не пишеться
	// напряму в DestRelPath.
	IsArchive bool
	SHA1      string
	SHA256    string
	SHA512    string
	SizeBytes int64
	UpdatedAt string
	// ManifestKey — унікальний ключ файлу в маніфесті (Path для workerFiles,
	// FileName для hashMods) — використовується для запису FileRecord у
	// InstallState після завершення.
	ManifestKey string
}

type QueueKind string

const (
	QueueHashMods QueueKind = "hash-mods" // Черга А
	QueueWorker   QueueKind = "worker"    // Черга B
)

// SyncPlan — результат обчислення дельти: що саме треба зробити для
// приведення локальної установки у відповідність до PackManifest.
type SyncPlan struct {
	HashModTasks    []Task   // Черга А
	WorkerTasks     []Task   // Черга B
	OnMissingTasks  []Task   // довстановлюються, якщо гравець видалив файл
	OrphansToRemove []string // локальні файли, яких більше немає в манифесті
	TotalBytes      int64
	// SkippedOnMissingFolders — override-теки з правилом on_missing/
	// first_install_only, які НЕ потрапили в OnMissingTasks, бо вже
	// існують на диску (правило спрацювало: тека НЕ буде перезаписана
	// цим синком). Лише для логування/діагностики — на сам план якання
	// не впливає.
	SkippedOnMissingFolders []string
}

// BuildPlan обчислює SyncPlan за формулою розділу 3.2 V2-документа:
//
//	remoteUpdatedAt новіший за locallyRecordedUpdatedAt?
//	  ні  → файл актуальний, 0 перевірок (найшвидший шлях)
//	  так → порахувати локальний хеш і порівняти з очікуваним
//	          збігається    → оновити тільки updatedAt в стані, не качати
//	          не збігається → перекачати файл
//
// hashExists — функція, яка рахує SHA файлу на диску (nil, якщо файлу
// немає). Винесена як параметр, щоб plan_test.go міг підміняти хешування
// без реального диску.
// BuildPlan обчислює SyncPlan (розділ 3 V2-документа): для КОЖНОГО
// hashMod і workerFile визначає — качати/пропустити/лише оновити
// updatedAt, а для локальних файлів, яких більше немає в маніфесті —
// формує список на видалення (осиротілі файли).
//
// hashExists — функція, яка рахує SHA файлу на диску (nil, якщо файлу
// немає). Винесена як параметр, щоб plan_test.go міг підміняти хешування
// без реального диску.
func BuildPlan(manifest *PackManifest, state *InstallState, localExists func(destRelPath string) bool,
	localHashMatches func(destRelPath, expectedHash string) bool) *SyncPlan {
	return buildPlanWithMods(manifest, state, localExists, localHashMatches, nil)
}

// buildPlanWithMods — BuildPlan + сканування папки mods/ для строгого
// видалення чужих модів. localMods — список jar-файлів у mods/ на диску
// (може бути nil, якщо сканування не потрібне/недоступне в тестах):
// будь-який jar, якого НЕМАЄ в маніфесті (ні в hashMods, ні серед
// bundled workerFiles) — додається в OrphansToRemove, бо в mods/ офіційної
// збірки має бути лише те, що надав R2 (вимога 2026-08-09: «лаунчер якщо
// бачить мод який завантажений користувачем то він буде видаляти такі
// моди»).
//
// Тонка обгортка без білого списку — для сумісності зі старими викликами
// й тестами. Реальна перевірка білого списку — buildPlanWithModsAndWhitelist.
func buildPlanWithMods(manifest *PackManifest, state *InstallState, localExists func(destRelPath string) bool,
	localHashMatches func(destRelPath, expectedHash string) bool, localMods func() []string) *SyncPlan {
	return buildPlanWithModsAndWhitelist(manifest, state, localExists, localHashMatches, localMods, nil)
}

// buildPlanWithModsAndWhitelist — те саме, що buildPlanWithMods, плюс
// isWhitelisted: перевірка «чи цей jar (за іменем файлу в mods/) дозволено
// лишити, навіть якщо його немає в маніфесті збірки». Викликач (Runner)
// будує isWhitelisted як замикання, що читає ID мода з жара
// (mods.ModIdentityOf) і звіряє з об'єднаним (глобальний + per-pack)
// білим списком — САМЕ по ID, а не по імені файлу: той самий мод різних
// версій має різне ім'я jar, але сталий id (вимога ТЗ). nil isWhitelisted
// = білий список порожній/недоступний — поведінка як була (строге
// видалення без винятків).
func buildPlanWithModsAndWhitelist(manifest *PackManifest, state *InstallState, localExists func(destRelPath string) bool,
	localHashMatches func(destRelPath, expectedHash string) bool, localMods func() []string,
	isWhitelisted func(fileName string) bool) *SyncPlan {

	plan := &SyncPlan{}
	seenKeys := map[string]bool{}

	for _, hm := range manifest.HashMods {
		seenKeys[hm.FileName] = true
		// DestPath заданий (шейдерпак/ресурспак з прямим CDN-URL, див.
		// HashMod.DestPath) — файл кладеться туди, а НЕ в mods/. Порожній —
		// звичайний мод, поведінка як завжди.
		destRel := "mods/" + hm.FileName
		if hm.DestPath != "" {
			destRel = hm.DestPath
		}
		rec, hasRec := state.Files[hm.FileName]

		needsCheck := !hasRec || isNewer(hm.UpdatedAt, rec.UpdatedAt) || !localExists(destRel)
		if !needsCheck {
			continue // швидкий шлях: не новіше за те, що вже встановлено — 0 перевірок
		}

		expectedHash := hm.SHA1
		if localExists(destRel) && expectedHash != "" && localHashMatches(destRel, expectedHash) {
			// Дата "новіша", але вміст фактично той самий (хибне спрацювання
			// дати) — файл НЕ перекачується, лише updatedAt буде оновлено
			// після синхронізації (застосовується у ApplyTaskResult).
			continue
		}

		plan.HashModTasks = append(plan.HashModTasks, Task{
			Queue:       QueueHashMods,
			URL:         hm.DownloadURL,
			DestRelPath: destRel,
			SHA1:        hm.SHA1,
			SHA512:      hm.SHA512,
			SizeBytes:   hm.FileSize,
			UpdatedAt:   hm.UpdatedAt,
			ManifestKey: hm.FileName,
		})
		plan.TotalBytes += hm.FileSize
	}

	for _, wf := range manifest.WorkerFiles {
		seenKeys[wf.Path] = true
		destRel := wf.DestPath()
		rec, hasRec := state.Files[wf.Path]

		// Для override-folder destRel — це ТЕКА (вміст архіву розпаковується
		// сюди). Перевіряємо, що вона існує: якщо гравець видалив розпаковану
		// теку — архів має перекачатись і розпакуватись заново.
		existsCheck := localExists(destRel)
		needsCheck := !hasRec || isNewer(wf.UpdatedAt, rec.UpdatedAt) || !existsCheck
		if !needsCheck {
			continue
		}

		if wf.Kind != KindOverrideFolder && wf.SHA256 != "" && localExists(destRel) &&
			localHashMatches(destRel, wf.SHA256) {
			continue // хибне спрацювання дати — вміст не змінився
		}

		size := wf.FileSize
		if wf.Kind == KindOverrideFolder {
			size = wf.ArchiveSize
		}

		plan.WorkerTasks = append(plan.WorkerTasks, Task{
			Queue:       QueueWorker,
			Kind:        wf.Kind,
			URL:         wf.Path, // додається базовий Worker URL пізніше
			DestRelPath: destRel,
			IsArchive:   wf.Kind == KindOverrideFolder,
			SHA256:      wf.SHA256,
			SizeBytes:   size,
			UpdatedAt:   wf.UpdatedAt,
			ManifestKey: wf.Path,
		})
		plan.TotalBytes += size
	}

	for _, omf := range manifest.OnMissingFiles {
		// ВАЖЛИВО: єдина умова пропуску — файл ЗАРАЗ є на диску (перше
		// встановлення чи гравець сам його поклав/налаштував). Прапорець
		// state.InstalledOnMissing НЕ використовується як блокування:
		// раніше він назавжди зупиняв повторне відновлення файлу навіть
		// якщо гравець (чи зламана збірка, чи видалення лаунчера) пізніше
		// втратив файл — options.txt тощо переставали відновлюватись
		// після першого встановлення. Тепер файл довстановлюється щоразу,
		// коли його фактично немає на диску, незалежно від історії.
		if localExists(omf.LocalPath) {
			continue // файл уже на диску — не чіпаємо (могли налаштувати)
		}
		plan.OnMissingTasks = append(plan.OnMissingTasks, Task{
			Queue:       QueueWorker,
			Kind:        KindOverrideFile,
			URL:         omf.Path,
			DestRelPath: omf.LocalPath,
			SHA256:      omf.SHA256,
			ManifestKey: omf.Path,
		})
	}

	// OnMissingFolders — override-теки (config/, resourcepacks/, shaderpacks/…)
	// з правилом on_missing / first_install_only: архів розпаковується ЛИШЕ
	// якщо теки ще немає на диску, і більше ніколи не перезаписується.
	// Логіка та сама, що в OnMissingFiles, але задача — архів (IsArchive=true),
	// бо качається .zip і розпаковується як тека (не файл).
	// РЕГРЕСІЯ 2026-08-09: лаунчер не читав manifest.OnMissingFolders взагалі
	// — папки config/ тощо з деплою ніколи не встановлювались.
	for _, omf := range manifest.OnMissingFolders {
		// Та сама зміна, що для OnMissingFiles вище: наявність теки на
		// диску ЗАРАЗ — єдина умова пропуску. state.InstalledOnMissing
		// більше не блокує відновлення: якщо тека (наприклад config/)
		// цілком зникла — це втрата даних, яку варто відновити з CDN,
		// а не залишати збірку без файлів назавжди після першого запуску.
		if localExists(omf.LocalPath) {
			// Тека вже на диску — правило on_missing/first_install_only
			// спрацювало як задумано: НЕ перезаписуємо. Записуємо в план
			// (не тільки в лог) — саме той факт, який зазвичай хочуть
			// перевірити "а точно не чіпає config?".
			plan.SkippedOnMissingFolders = append(plan.SkippedOnMissingFolders, omf.LocalPath)
			continue
		}
		plan.OnMissingTasks = append(plan.OnMissingTasks, Task{
			Queue:       QueueWorker,
			Kind:        KindOverrideFolder,
			URL:         omf.Path,
			DestRelPath: omf.LocalPath,
			IsArchive:   true,
			SHA256:      omf.SHA256,
			SizeBytes:   omf.FileSize,
			ManifestKey: omf.Path,
		})
		plan.TotalBytes += omf.FileSize
	}

	// ЗАХИСТ від видалення як осиротілого (БАГ: servers.dat зникав після оновлення
	// збірки). Файл/тека з правилом on_missing/first_install_only належить
	// ГРАВЦЮ, а не маніфесту як «керований» контент. Але в State.Files міг
	// лишитись запис від часів, коли цей файл ішов у workerFiles (або запис,
	// який лаунчер сам створив після довстановлення теки — markArchiveComplete),
	// з ключем packs/<id>/overrides/servers.dat. Оскільки on_missing-ключі не
	// потрапляли в seenKeys, цей запис вважався «осиротілим» і файл видалявся
	// з диска гравця. Тепер: (а) on_missing-ключі вважаються «баченими»,
	// (б) шляхи on_missing/user-data взагалі ніколи не потрапляють у
	// OrphansToRemove.
	protected := map[string]bool{}
	for _, omf := range manifest.OnMissingFiles {
		seenKeys[omf.Path] = true
		protected[normProtectedPath(omf.LocalPath)] = true
	}
	for _, omf := range manifest.OnMissingFolders {
		seenKeys[omf.Path] = true
		protected[normProtectedPath(omf.LocalPath)] = true
	}

	// Осиротілі файли: були в попередньому стані, але зникли з маніфесту
	// (мод прибрали зі збірки чи перенесли hash<->bundled). Видаляємо за
	// ЛОКАЛЬНИМ шляхом (rec.LocalPath): раніше тут був rec.Path — R2-ключ
	// ("packs/<id>/mods/x.jar") чи FileName без префікса "mods/", по якому
	// os.Remove нічого не знаходив і моди лишались на диску назавжди.
	for key, rec := range state.Files {
		if seenKeys[key] {
			continue
		}
		local := rec.LocalPath
		if local == "" {
			// Старий стан (до фіксу LocalPath): fallback — R2-ключ, що
			// закінчується на jar у mods/, або FileName → mods/<name>.
			if strings.HasPrefix(rec.Path, "mods/") {
				local = rec.Path
			} else if strings.HasSuffix(rec.Path, ".jar") {
				local = "mods/" + filepath.Base(rec.Path)
			} else {
				local = rec.Path
			}
		}
		if isProtectedUserPath(local, protected) {
			continue // файл гравця (servers.dat, options.txt, on_missing-шляхи) — не видаляємо
		}
		plan.OrphansToRemove = append(plan.OrphansToRemove, local)
	}

	// Строге видалення чужих модів: jar у mods/, якого немає в маніфесті
	// (ні hashMod, ні bundled) — видаляємо, ЯКЩО він не в білому списку.
	// У mods/ офіційної збірки має бути лише те, що надав R2, АБО те, що
	// користувач/адмін явно дозволив білим списком (ТЗ: «лаунчер якщо
	// бачить цей мод під час перевірки цілісності файлів... не буде його
	// видаляти бо він у білому списку»). Файли поза mods/ (conf-и тощо)
	// не чіпаємо.
	//
	// ВАЖЛИВО (Windows): ФС Windows реєстро-НЕзалежна — користувач міг
	// перейменувати FancyMod.jar у fancymod.jar, або R2 віддав ім'я з
	// іншим регістром. Порівнюємо імена через EqualFold (case-insensitive),
	// інакше такий мод був би ПОМИЛКОВО видалений як «чужий» (деструктивна
	// операція — хибне спрацювання неприпустиме).
	if localMods != nil {
		modNames := map[string]bool{}
		for _, hm := range manifest.HashMods {
			// DestPath заданий — файл НЕ належить mods/ (шейдерпак/
			// ресурспак, див. HashMod.DestPath) — не додаємо в "легальні"
			// імена для mods/, інакше строге сканування пропустило б
			// такий файл, якби він колись (наприклад через стару версію
			// лаунчера до фіксу класифікації) опинився в mods/ помилково.
			if hm.DestPath != "" {
				continue
			}
			modNames[strings.ToLower(hm.FileName)] = true
		}
		for _, wf := range manifest.WorkerFiles {
			if wf.Kind == KindBundledMod && wf.FileName != "" {
				modNames[strings.ToLower(wf.FileName)] = true
			}
		}
		for _, name := range localMods() {
			if modNames[strings.ToLower(name)] {
				continue
			}
			// Не в маніфесті — але, можливо, у білому списку (перевірка по
			// mod-id усередині jar, isWhitelisted сам це робить — тут лише
			// викликаємо, plan.go не знає нічого про формат jar).
			if isWhitelisted != nil && isWhitelisted(name) {
				continue
			}
			plan.OrphansToRemove = append(plan.OrphansToRemove, "mods/"+name)
		}
	}

	return plan
}

// isNewer — порівняння двох ISO-8601 рядків часу як рядків працює коректно
// для RFC3339 (лексикографічний порядок збігається з хронологічним), але
// про всяк випадок парсимо явно, щоб не покладатись на це навмисно.
func isNewer(remote, local string) bool {
	if remote == "" {
		return false
	}
	if local == "" {
		return true
	}
	rt, err1 := parseTime(remote)
	lt, err2 := parseTime(local)
	if err1 != nil || err2 != nil {
		return remote > local // fallback: лексикографічне порівняння RFC3339
	}
	return rt.After(lt)
}

// userDataFiles — файли, які створює/редагує сама гра чи гравець. Лаунчер
// НІКОЛИ не видаляє їх як «осиротілі», навіть якщо вони колись були в
// маніфесті (напр. збірка раніше віддавала servers.dat як workerFile).
var userDataFiles = map[string]bool{
	"servers.dat":        true,
	"servers.dat_old":    true,
	"options.txt":        true,
	"optionsof.txt":      true,
	"optionsshaders.txt": true,
}

func normProtectedPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return strings.ToLower(strings.TrimRight(p, "/"))
}

// isProtectedUserPath — true, якщо локальний шлях не можна видаляти як
// осиротілий: це файл гравця або шлях/вміст теки з правилом on_missing.
func isProtectedUserPath(local string, protected map[string]bool) bool {
	n := normProtectedPath(local)
	if n == "" {
		return false
	}
	if userDataFiles[n] || protected[n] {
		return true
	}
	// файл усередині on_missing-теки (config/foo.toml при правилі config/)
	for pr := range protected {
		if pr != "" && strings.HasPrefix(n, pr+"/") {
			return true
		}
	}
	return false
}
