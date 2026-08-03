package sync

import "time"

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
func BuildPlan(manifest *PackManifest, state *InstallState, localExists func(destRelPath string) bool,
	localHashMatches func(destRelPath, expectedHash string) bool) *SyncPlan {

	plan := &SyncPlan{}
	seenKeys := map[string]bool{}

	for _, hm := range manifest.HashMods {
		seenKeys[hm.FileName] = true
		destRel := "mods/" + hm.FileName
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

		existsCheck := wf.Kind == KindOverrideFolder || localExists(destRel)
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
		if state.InstalledOnMissing[omf.LocalPath] {
			continue // гравець уже мав цей файл раніше (можливо, видалив навмисно)
		}
		if localExists(omf.LocalPath) {
			continue // файл уже на диску (перше встановлення чи гравець сам додав)
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

	// Осиротілі bundled-моди: були в попередньому стані, але зникли з
	// маніфесту (мод прибрали зі збірки чи перенесли hash<->bundled).
	for key, rec := range state.Files {
		if seenKeys[key] {
			continue
		}
		// Прибираємо тільки файли, які лаунчер сам колись встановив (є запис
		// у FileRecord) — файли гравця (не в state.Files) ніколи не чіпаються.
		plan.OrphansToRemove = append(plan.OrphansToRemove, rec.Path)
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
