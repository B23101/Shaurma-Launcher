package sync

import (
	"testing"
)

func TestBuildPlanArchiveNotReplannedWhenTracked(t *testing.T) {
	manifest := &PackManifest{
		ID:          "test-pack",
		UpdatedAt:   "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{},
	}

	// Один override-folder архів, розпакований у config/.
	manifest.WorkerFiles = append(manifest.WorkerFiles, WorkerFile{
		Kind:        KindOverrideFolder,
		LocalPath:   "config/",
		Path:        "configs/my-config.zip",
		SHA256:      "aabb",
		ArchiveSize: 100,
		UpdatedAt:   "2026-08-01T10:00:00Z",
	})

	state := &InstallState{
		PackID: "test-pack",
		Files:  map[string]FileRecord{},
	}

	// ВИПРАВЛЕННЯ №1 (регресійний тест): після розпаковки архів записується
	// в State.Files (markArchiveComplete), тож повторний BuildPlan НЕ має
	// планувати архів знову, якщо тека існує і updatedAt не змінився.
	// Раніше markTaskComplete повертав рано для IsArchive, State.Files не
	// мав запису, і архів перекачувався при КОЖНОМУ запуску гри.
	state.Files["configs/my-config.zip"] = FileRecord{
		Path:      "configs/my-config.zip",
		SHA:       "aabb",
		UpdatedAt: "2026-08-01T10:00:00Z",
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return destRelPath == "config/" }, // тека існує
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.WorkerTasks) != 0 {
		t.Fatalf("архів, що вже розпакований і трекнутий, не має плануватись знову; отримали %d задач: %+v", len(plan.WorkerTasks), plan.WorkerTasks)
	}
}

func TestBuildPlanArchiveReplannedWhenFolderMissing(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{
			{
				Kind:        KindOverrideFolder,
				LocalPath:   "config/",
				Path:        "configs/my-config.zip",
				SHA256:      "aabb",
				ArchiveSize: 100,
				UpdatedAt:   "2026-08-01T10:00:00Z",
			},
		},
	}

	state := &InstallState{
		PackID: "test-pack",
		Files: map[string]FileRecord{
			"configs/my-config.zip": {
				Path:      "configs/my-config.zip",
				SHA:       "aabb",
				UpdatedAt: "2026-08-01T10:00:00Z",
			},
		},
	}

	// Гравець видалив розпаковану теку — архів має перекачатись заново.
	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return false }, // теки НЕМАЄ
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.WorkerTasks) != 1 {
		t.Fatalf("архів має плануватись знову, коли теки немає; отримали %d задач", len(plan.WorkerTasks))
	}
	task := plan.WorkerTasks[0]
	if !task.IsArchive {
		t.Fatalf("задача має бути архівом (IsArchive=true), отримали: %+v", task)
	}
	if task.DestRelPath != "config/" {
		t.Fatalf("DestRelPath = %q, очікувалось config/", task.DestRelPath)
	}
}

// TestBuildPlanOnMissingFoldersPlannedWhenMissing — регресія 2026-08-09:
// лаунчер НЕ читав manifest.OnMissingFolders (папки config/ тощо з правилом
// on_missing/first_install_only), тож вони ніколи не встановлювались.
// Тепер: якщо теки немає на диску і вона не трекалась як встановлена —
// має з'явитись архівна задача (IsArchive=true).
func TestBuildPlanOnMissingFoldersPlannedWhenMissing(t *testing.T) {
	manifest := &PackManifest{
		ID:          "test-pack",
		UpdatedAt:   "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{},
		OnMissingFolders: []OnMissingFolder{
			{LocalPath: "config/", Path: "packs/test-pack/override-folders-once/config.zip", SHA256: "aabb", FileSize: 512},
		},
	}

	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return false }, // теки НЕМАЄ
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.OnMissingTasks) != 1 {
		t.Fatalf("onMissingFolder без теки має плануватись; отримали %d задач", len(plan.OnMissingTasks))
	}
	task := plan.OnMissingTasks[0]
	if !task.IsArchive {
		t.Fatalf("onMissingFolder має бути архівом (IsArchive=true), отримали: %+v", task)
	}
	if task.DestRelPath != "config/" {
		t.Fatalf("DestRelPath = %q, очікувалось config/", task.DestRelPath)
	}
	if task.SHA256 != "aabb" {
		t.Fatalf("SHA256 = %q, очікувалось aabb", task.SHA256)
	}
	if plan.TotalBytes != 512 {
		t.Fatalf("TotalBytes = %d, очікувалось 512 (FileSize архіву)", plan.TotalBytes)
	}
}

// TestBuildPlanOnMissingFoldersSkippedWhenExists — тека вже на диску (гравець
// сам додав контент, або перше встановлення її принесло) — архів НЕ качається.
func TestBuildPlanOnMissingFoldersSkippedWhenExists(t *testing.T) {
	manifest := &PackManifest{
		ID:          "test-pack",
		UpdatedAt:   "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{},
		OnMissingFolders: []OnMissingFolder{
			{LocalPath: "config/", Path: "packs/test-pack/override-folders-once/config.zip", SHA256: "aabb"},
		},
	}

	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return destRelPath == "config/" }, // тека Є
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.OnMissingTasks) != 0 {
		t.Fatalf("теку, що існує, не має довстановлюватись; отримали %d задач", len(plan.OnMissingTasks))
	}
}

// TestBuildPlanOnMissingFoldersRestoredWhenFolderLost — гравець уже мав цю
// теку (InstalledOnMissing=true), але зараз її НЕМАЄ на диску: правило
// on_missing/першого встановлення вже спрацювало колись, проте тека зникла
// (видалив гравець, зламалась збірка, переустановка лаунчера) — тому тека
// МАЄ довстановитись, а не лишити збірку без файлів назавжди.
//
// Саме так поводиться код із 2026-08-09 (див. коментар у plan.go): єдина
// умова пропуску — тека ФІЗИЧНО є на диску зараз. Раніше цей тест
// закріплював зворотне (InstalledOnMissing назавжди блокував відновлення),
// і тому падав на здоровому коді.
func TestBuildPlanOnMissingFoldersRestoredWhenFolderLost(t *testing.T) {
	manifest := &PackManifest{
		ID:          "test-pack",
		UpdatedAt:   "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{},
		OnMissingFolders: []OnMissingFolder{
			{LocalPath: "config/", Path: "packs/test-pack/override-folders-once/config.zip", SHA256: "aabb"},
		},
	}

	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{"config/": true},
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return false },
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.OnMissingTasks) != 1 {
		t.Fatalf("зниклу теку треба довстановити; отримали %d задач", len(plan.OnMissingTasks))
	}
	if task := plan.OnMissingTasks[0]; task.DestRelPath != "config/" || !task.IsArchive {
		t.Fatalf("задача має бути архівом для config/, отримали: %+v", task)
	}
	if len(plan.SkippedOnMissingFolders) != 0 {
		t.Fatalf("теки немає на диску — пропусків бути не має; отримали %v", plan.SkippedOnMissingFolders)
	}
}

// TestBuildPlanStrictModsRemovesForeignJars — вимога 2026-08-09: у mods/
// офіційної збірки має бути лише те, що надав R2. Будь-який jar, якого
// немає в маніфесті (ні hashMod, ні bundled), — має піти в OrphansToRemove
// (видалитись).
func TestBuildPlanStrictModsRemovesForeignJars(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		HashMods: []HashMod{
			{FileName: "from-modrinth.jar", SHA1: "aabb", UpdatedAt: "2026-08-01T10:00:00Z"},
		},
		WorkerFiles: []WorkerFile{
			{Kind: KindBundledMod, FileName: "from-worker.jar", Path: "packs/test-pack/mods/from-worker.jar", SHA256: "ccdd", UpdatedAt: "2026-08-01T10:00:00Z"},
		},
	}
	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}

	plan := buildPlanWithMods(manifest, state,
		func(destRelPath string) bool { return false },
		func(destRelPath, expectedHash string) bool { return false },
		// Локально лежить 3 jar: 2 з маніфесту + 1 чужий (гравець доклав).
		func() []string { return []string{"from-modrinth.jar", "from-worker.jar", "player-added.jar"} },
	)

	found := false
	for _, o := range plan.OrphansToRemove {
		if o == "mods/player-added.jar" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("чужий мод player-added.jar має бути в OrphansToRemove; отримали: %v", plan.OrphansToRemove)
	}
	for _, o := range plan.OrphansToRemove {
		if o == "mods/from-modrinth.jar" || o == "mods/from-worker.jar" {
			t.Fatalf("мод з маніфесту (%s) не має видалятись; orphans: %v", o, plan.OrphansToRemove)
		}
	}
}

// TestBuildPlanWhitelistProtectsForeignJars — ТЗ: «лаунчер якщо бачить
// цей мод... у білому списку — не буде його видаляти». isWhitelisted
// звіряється за іменем файлу (сам колбек усередині читає mod-id з jar —
// тут імітуємо результат цієї перевірки напряму, без реального zip).
// player-added.jar (whitelisted) НЕ має видалятись, another-foreign.jar
// (НЕ whitelisted) — має, як і раніше (регресія попереднього тесту).
func TestBuildPlanWhitelistProtectsForeignJars(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		HashMods: []HashMod{
			{FileName: "from-modrinth.jar", SHA1: "aabb", UpdatedAt: "2026-08-01T10:00:00Z"},
		},
	}
	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}

	plan := buildPlanWithModsAndWhitelist(manifest, state,
		func(destRelPath string) bool { return false },
		func(destRelPath, expectedHash string) bool { return false },
		func() []string { return []string{"from-modrinth.jar", "player-added.jar", "another-foreign.jar"} },
		func(fileName string) bool { return fileName == "player-added.jar" },
	)

	whitelisted, foreign := false, false
	for _, o := range plan.OrphansToRemove {
		if o == "mods/player-added.jar" {
			whitelisted = true
		}
		if o == "mods/another-foreign.jar" {
			foreign = true
		}
	}
	if whitelisted {
		t.Fatalf("player-added.jar у білому списку — НЕ має бути в OrphansToRemove; отримали: %v", plan.OrphansToRemove)
	}
	if !foreign {
		t.Fatalf("another-foreign.jar НЕ у білому списку — має бути в OrphansToRemove; отримали: %v", plan.OrphansToRemove)
	}
}

// TestBuildPlanHashModDestPath — регресія: шейдерпак/ресурспак з прямим
// CDN-URL (Modrinth хостить і їх так само, як моди — теж є в hashMods,
// Черга А) має якатись за HashMod.DestPath, а НЕ жорстко в mods/. Раніше
// DestPath не існував, і будь-який HashMod беззастережно йшов у
// "mods/"+FileName — шейдерпак опинявся серед звичайних модів.
func TestBuildPlanHashModDestPath(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		HashMods: []HashMod{
			{FileName: "SomeShader.zip", DestPath: "shaderpacks/SomeShader.zip", SHA1: "aabb", UpdatedAt: "2026-08-01T10:00:00Z"},
			{FileName: "regular-mod.jar", SHA1: "ccdd", UpdatedAt: "2026-08-01T10:00:00Z"},
		},
	}
	state := &InstallState{
		PackID:             "test-pack",
		Files:              map[string]FileRecord{},
		InstalledOnMissing: map[string]bool{},
	}

	plan := buildPlanWithMods(manifest, state,
		func(destRelPath string) bool { return false },
		func(destRelPath, expectedHash string) bool { return false },
		// mods/ на диску реально містить ЛИШЕ звичайний мод — шейдер туди
		// ніколи не якався (сканується окремо, не через localMods).
		func() []string { return []string{"regular-mod.jar"} },
	)

	var shaderTask, modTask *Task
	for i := range plan.HashModTasks {
		switch plan.HashModTasks[i].ManifestKey {
		case "SomeShader.zip":
			shaderTask = &plan.HashModTasks[i]
		case "regular-mod.jar":
			modTask = &plan.HashModTasks[i]
		}
	}
	if shaderTask == nil {
		t.Fatal("задача для SomeShader.zip не знайдена")
	}
	if shaderTask.DestRelPath != "shaderpacks/SomeShader.zip" {
		t.Fatalf("шейдер має якатись у shaderpacks/, отримали DestRelPath=%q", shaderTask.DestRelPath)
	}
	if modTask == nil {
		t.Fatal("задача для regular-mod.jar не знайдена")
	}
	if modTask.DestRelPath != "mods/regular-mod.jar" {
		t.Fatalf("звичайний мод (без DestPath) має якатись у mods/, отримали DestRelPath=%q", modTask.DestRelPath)
	}

	// Строге сканування mods/ не повинно вважати шейдер "легальним" файлом
	// mods/ — інакше якби він там колись опинився (стара версія лаунчера
	// до фіксу), він ніколи б не видалився як "чужий".
	for _, o := range plan.OrphansToRemove {
		if o == "mods/regular-mod.jar" {
			t.Fatalf("regular-mod.jar є в маніфесті — не має бути orphan; orphans: %v", plan.OrphansToRemove)
		}
	}
}

// TestBuildPlanOrphansUseLocalPath — регресія 2026-08-09: OrphansToRemove
// має містити ЛОКАЛЬНИЙ шлях ("mods/foo.jar"), а не R2-ключ
// ("packs/<id>/mods/foo.jar") — бо removeOrphans видаляє саме за ним.
func TestBuildPlanOrphansUseLocalPath(t *testing.T) {
	manifest := &PackManifest{ID: "test-pack", UpdatedAt: "2026-08-01T10:00:00Z"}
	state := &InstallState{
		PackID:             "test-pack",
		InstalledOnMissing: map[string]bool{},
		Files: map[string]FileRecord{
			// Запис з LocalPath (новий формат) — має взятись саме він.
			"packs/test-pack/mods/gone.jar": {
				Path: "packs/test-pack/mods/gone.jar", LocalPath: "mods/gone.jar", SHA: "x",
			},
			// Старий формат без LocalPath: R2-ключ → fallback mods/<base>.
			"packs/test-pack/mods/old.jar": {
				Path: "packs/test-pack/mods/old.jar", SHA: "y",
			},
		},
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return false },
		func(destRelPath, expectedHash string) bool { return false },
	)

	hasLocal := false
	hasFallback := false
	for _, o := range plan.OrphansToRemove {
		if o == "mods/gone.jar" {
			hasLocal = true
		}
		if o == "mods/old.jar" {
			hasFallback = true
		}
	}
	if !hasLocal {
		t.Fatalf("LocalPath-orphan не потрапив у OrphansToRemove: %v", plan.OrphansToRemove)
	}
	if !hasFallback {
		t.Fatalf("fallback-orphan (R2-ключ → mods/) не потрапив: %v", plan.OrphansToRemove)
	}
}

func TestBuildPlanArchiveReplannedWhenUpdated(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		WorkerFiles: []WorkerFile{
			{
				Kind:        KindOverrideFolder,
				LocalPath:   "config/",
				Path:        "configs/my-config.zip",
				SHA256:      "newhash",
				ArchiveSize: 100,
				UpdatedAt:   "2026-08-02T10:00:00Z", // новіша за запис стану
			},
		},
	}

	state := &InstallState{
		PackID: "test-pack",
		Files: map[string]FileRecord{
			"configs/my-config.zip": {
				Path:      "configs/my-config.zip",
				SHA:       "aabb",
				UpdatedAt: "2026-08-01T10:00:00Z",
			},
		},
	}

	plan := BuildPlan(manifest, state,
		func(destRelPath string) bool { return true },
		func(destRelPath, expectedHash string) bool { return false },
	)

	if len(plan.WorkerTasks) != 1 {
		t.Fatalf("оновлений архів має перекачатись; отримали %d задач", len(plan.WorkerTasks))
	}
}

// TestBuildPlanDoesNotRemoveServersDatAsOrphan — регресія: після оновлення
// збірки servers.dat зникав. Раніше файл ішов у workerFiles (запис у
// State.Files з ключем packs/<id>/overrides/servers.dat), потім правило
// стало on_missing → файл перейшов в onMissingFiles, ключ випав із seenKeys
// і запис вважався «осиротілим» → servers.dat видалявся з диска гравця.
func TestBuildPlanDoesNotRemoveServersDatAsOrphan(t *testing.T) {
	manifest := &PackManifest{
		ID:        "test-pack",
		UpdatedAt: "2026-08-01T10:00:00Z",
		OnMissingFiles: []OnMissingFile{
			{LocalPath: "servers.dat", Path: "packs/test-pack/overrides/servers.dat", SHA256: "aa"},
		},
	}
	state := &InstallState{
		PackID: "test-pack",
		Files: map[string]FileRecord{
			// Старий запис + запис без LocalPath/з іншим ключем.
			"packs/test-pack/overrides/servers.dat": {Path: "packs/test-pack/overrides/servers.dat", LocalPath: "servers.dat"},
			"packs/test-pack/old/servers.dat":       {Path: "packs/test-pack/old/servers.dat", LocalPath: "servers.dat"},
			"packs/test-pack/old/options.txt":       {Path: "packs/test-pack/old/options.txt", LocalPath: "options.txt"},
			"packs/test-pack/old/config.zip":        {Path: "packs/test-pack/old/config.zip", LocalPath: "config/foo.toml"},
			"packs/test-pack/old/gone.txt":          {Path: "packs/test-pack/old/gone.txt", LocalPath: "kubejs/gone.txt"},
		},
		InstalledOnMissing: map[string]bool{},
	}
	plan := BuildPlan(manifest, state,
		func(string) bool { return true }, // файли гравця вже на диску
		func(string, string) bool { return true },
	)
	for _, o := range plan.OrphansToRemove {
		switch o {
		case "servers.dat", "options.txt":
			t.Fatalf("файл гравця %q не має видалятись як осиротілий: %v", o, plan.OrphansToRemove)
		}
	}
	// Справжній осиротілий файл керованої збірки — видаляється як і раніше.
	found := false
	for _, o := range plan.OrphansToRemove {
		if o == "kubejs/gone.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("звичайний осиротілий файл має лишитись у OrphansToRemove: %v", plan.OrphansToRemove)
	}
}
