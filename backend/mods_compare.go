package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Порівнювач модів двох збірок ──────────────────────────────────────────
// На відміну від AnalyzeModMigration (одностороння міграція: джерело → ціль),
// CompareModPacks симетричний: обидві збірки скануються одночасно, кожен
// мод отримує рядок з ОБОМА сторонами, і встановити/оновити можна в БУДЬ-ЯКУ
// зі сторін окремо (InstallCompareMod з явним targetPackId).
//
// Резолвер версій (sha1-batch, назва-пошук Modrinth → CurseForge fallback,
// локальне копіювання при однаковому loader+MC) перевикористовується з
// mods_migration.go — та сама мережева інфраструктура. Головна відмінність
// від міграції: тут явно розрізняються ДВА різних "не можна встановити":
//   - мода взагалі немає на Modrinth/CurseForge (CanInstall=false, CompatNote="")
//   - мод є на маркетплейсі, але автор не випустив версію під loader/MC
//     САМЕ ЦІЄЇ сторони (CanInstall=false, CompatNote="noCompatVersion")
// Одностороння міграція обидва випадки зводила до "not-found", бо там
// цілей резолву лише одна (ціль міграції). У порівнювачі кожна зі сторін
// може мати СВІЙ loader/MC (дві різні збірки), тому "знайдено, але не під
// цю версію" — окремий, важливий для UI випадок (інша іконка/текст).

// compareModrinthResolve — результат резолву одного мода під конкретну
// пару (mcVersion, loader) — спільний для лівої і правої сторони.
type compareModrinthResolve struct {
	found       bool // мод знайдено на Modrinth/CurseForge (проект існує)
	compatible  bool // і є версія під саме цю mcVersion/loader
	projectID   string
	projectSlug string
	name        string
	iconURL     string
	source      string // modrinth | curseforge
	version     string
	filename    string
	url         string
	sha1        string
}

// CompareModPacks будує двосторонній звіт порівняння модів leftPackID і
// rightPackID. Матчинг рядків — за modId з jar-метаданих (найнадійніший,
// не залежить від мережі); коли modId порожній з одного боку — fallback
// на sha1, а як останній варіант — на нормалізовану назву файлу.
func (a *App) CompareModPacks(leftPackID, rightPackID string) (model.ModCompareReport, error) {
	var report model.ModCompareReport
	report.Rows = []model.ModCompareRow{}

	leftPack, leftOK := a.migrationPackByID(leftPackID)
	rightPack, rightOK := a.migrationPackByID(rightPackID)

	leftMods, _ := a.modMgr.ScanModsFull(a.instanceModsDir(leftPackID))
	rightMods, _ := a.modMgr.ScanModsFull(a.instanceModsDir(rightPackID))

	sameEnv := leftOK && rightOK &&
		leftPack.Loader != "" && rightPack.Loader != "" &&
		strings.EqualFold(leftPack.Loader, rightPack.Loader) &&
		leftPack.MCVersion != "" && leftPack.MCVersion == rightPack.MCVersion
	report.SameLoaderVersion = sameEnv

	// ── Матчинг рядків: modId → sha1 → normalized filename ──────────────
	type pairKey = string
	leftByKey := map[pairKey]*model.ModEntry{}
	rightByKey := map[pairKey]*model.ModEntry{}
	keyOrder := []pairKey{}
	seenKey := map[pairKey]bool{}

	keyFor := func(m *model.ModEntry) pairKey {
		if m.ID != "" {
			return "id:" + m.ID
		}
		if m.Sha1 != "" {
			return "sha1:" + strings.ToLower(m.Sha1)
		}
		return "name:" + strings.ToLower(baseNameNoExtCompare(m.FileName))
	}
	for i := range leftMods {
		k := keyFor(&leftMods[i])
		leftByKey[k] = &leftMods[i]
		if !seenKey[k] {
			seenKey[k] = true
			keyOrder = append(keyOrder, k)
		}
	}
	for i := range rightMods {
		k := keyFor(&rightMods[i])
		rightByKey[k] = &rightMods[i]
		if !seenKey[k] {
			seenKey[k] = true
			keyOrder = append(keyOrder, k)
		}
	}

	// ── Batch sha1-резолв обох сторін одразу (одна мережева хвиля) ──────
	// Резолвимо кожен унікальний sha1 з обох тек одним batch-запитом, щоб
	// не робити окремий похід в мережу на кожну сторону кожного рядка.
	var allHashes []string
	hashSeen := map[string]bool{}
	collectHashes := func(mods []model.ModEntry) {
		for _, m := range mods {
			if m.Sha1 != "" {
				h := strings.ToLower(m.Sha1)
				if !hashSeen[h] {
					hashSeen[h] = true
					allHashes = append(allHashes, h)
				}
			}
		}
	}
	collectHashes(leftMods)
	collectHashes(rightMods)

	versionsByHash, _ := a.modrinth.GetVersionsByHashes(allHashes)
	projIDs := []string{}
	seenProj := map[string]bool{}
	for _, v := range versionsByHash {
		if v.ProjectID != "" && !seenProj[v.ProjectID] {
			seenProj[v.ProjectID] = true
			projIDs = append(projIDs, v.ProjectID)
		}
	}
	projects, _ := a.modrinth.GetProjectsBatch(projIDs)

	// ── Резолв "чого бракує" для однієї сторони одного рядка ────────────
	// mFrom — мод, який Є (з протилежної сторони, звідки можна перенести);
	// targetMC/targetLoader — MC/loader СТОРОНИ, куди намагаємось поставити.
	var resolveMu sync.Mutex
	resolveCache := map[string]compareModrinthResolve{}
	resolve := func(mFrom *model.ModEntry, targetMC, targetLoader string) compareModrinthResolve {
		cacheKey := mFrom.FileName + "|" + targetMC + "|" + targetLoader
		resolveMu.Lock()
		if r, ok := resolveCache[cacheKey]; ok {
			resolveMu.Unlock()
			return r
		}
		resolveMu.Unlock()

		var res compareModrinthResolve

		// 1) Точний sha1-збіг (з batch, зроблено вище).
		if mFrom.Sha1 != "" {
			if v, ok := versionsByHash[strings.ToLower(mFrom.Sha1)]; ok && v.ProjectID != "" {
				res.found = true
				res.projectID = v.ProjectID
				res.source = "modrinth"
				if p, ok := projects[v.ProjectID]; ok {
					res.projectSlug = p.Slug
					res.name = p.Title
					res.iconURL = p.IconURL
				}
				if fullVersionCompatible(v, targetMC, targetLoader) {
					res.compatible = true
					res.version = v.VersionNumber
					for _, f := range v.Files {
						if f.Primary || res.filename == "" {
							res.filename = f.Filename
							res.url = f.URL
							res.sha1 = f.Sha1
						}
						if f.Primary {
							break
						}
					}
				} else if opt := a.modrinthLatest(v.ProjectID, targetMC, targetLoader); opt != nil {
					res.compatible = true
					res.version = opt.VersionNumber
					res.filename = opt.Filename
					res.url = opt.URL
					res.sha1 = opt.Sha1
				}
				// found=true, compatible=false лишається — "знайдено на
				// маркетплейсі, але немає версії під цю MC/loader".
				resolveMu.Lock()
				resolveCache[cacheKey] = res
				resolveMu.Unlock()
				return res
			}
		}

		// 2) Пошук за назвою — Modrinth (уже фільтрує mcVersion/loader
		// цілі під час пошуку, тож знайдений проект гарантовано сумісний),
		// фолбек CurseForge.
		if projID, ok := a.findModOnModrinth(mFrom, targetMC, targetLoader); ok {
			res.found = true
			res.compatible = true
			res.projectID = projID
			res.source = "modrinth"
			if p, err := a.modrinth.GetProject(projID); err == nil && p != nil {
				res.projectSlug = p.Slug
				res.iconURL = p.IconURL
				res.name = p.Title
			}
			if opt := a.modrinthLatest(projID, targetMC, targetLoader); opt != nil {
				res.version = opt.VersionNumber
				res.filename = opt.Filename
				res.url = opt.URL
				res.sha1 = opt.Sha1
			} else {
				// Пошук за назвою знайшов проект, але per-project резолв
				// сумісної версії не зміг її дістати (нечастий рант-кейс —
				// напр. видалена версія між пошуком і резолвом) —
				// чесніше показати "немає сумісної версії", ніж мовчки
				// пропонувати встановити невідомо що.
				res.compatible = false
			}
			resolveMu.Lock()
			resolveCache[cacheKey] = res
			resolveMu.Unlock()
			return res
		}
		if cf, err := a.findModOnCurseForge(mFrom, targetMC, targetLoader); err == nil && cf != nil {
			res.found = true
			res.compatible = true
			res.projectID = fmt.Sprintf("%d", cf.ID)
			res.source = "curseforge"
			res.projectSlug = cf.Slug
			res.iconURL = cf.LogoURL
			res.name = cf.Name
			res.version = cf.FileName
			res.filename = cf.FileName
			res.url = cf.DownloadURL
		}

		resolveMu.Lock()
		resolveCache[cacheKey] = res
		resolveMu.Unlock()
		return res
	}

	// buildSide — сторона рядка, де мод Є (present/outdated).
	buildSide := func(m *model.ModEntry) model.ModCompareSide {
		return model.ModCompareSide{
			Status:   "present",
			FileName: m.FileName,
			Version:  m.Version,
			Enabled:  m.Enabled,
			Sha1:     m.Sha1,
			ModID:    m.ID,
		}
	}

	// buildMissingSide — сторона рядка, де мода НЕМАЄ: якщо loader+MC
	// обох збірок однакові — пряме копіювання (без мережі), інакше —
	// мережевий резолв під loader/MC ЦІЄЇ сторони. sourcePackID — packId
	// протилежної сторони (звідки береться файл при LocalCopy).
	buildMissingSide := func(mFrom *model.ModEntry, targetPack model.MigrationPack, targetPackOK bool, sourcePackID string) model.ModCompareSide {
		side := model.ModCompareSide{Status: "missing"}
		if mFrom == nil || !targetPackOK {
			return side
		}
		if sameEnv {
			side.CanInstall = true
			side.LocalCopy = true
			side.ResolvedName = mFrom.Name
			side.ResolvedVersion = mFrom.Version
			side.ResolvedFile = mFrom.FileName
			side.SourcePackID = sourcePackID
			side.SourceFileName = mFrom.FileName
			return side
		}
		r := resolve(mFrom, targetPack.MCVersion, targetPack.Loader)
		if !r.found {
			side.CanInstall = false
			return side
		}
		if !r.compatible {
			side.CanInstall = false
			side.CompatNote = "noCompatVersion"
			return side
		}
		side.CanInstall = true
		side.Source = r.source
		side.ProjectID = r.projectID
		side.ProjectSlug = r.projectSlug
		side.ResolvedName = firstNonEmpty(r.name, mFrom.Name)
		side.ResolvedVersion = r.version
		side.ResolvedFile = r.filename
		side.ResolvedURL = r.url
		side.ResolvedSha1 = r.sha1
		return side
	}

	// ── Крок 1: побудова рядків БЕЗ мережі (миттєво) ────────────────────
	// Матч є з обох боків (present/outdated) або localCopy-збіг (sameEnv)
	// не потребують мережевого резолву взагалі — рахуємо їх одразу.
	// Мережевого резолву (findModOnModrinth/findModOnCurseForge — пошук
	// за назвою, коли sha1 не збігся) потребують лише рядки "тільки
	// зліва"/"тільки справа" з РІЗНИМ loader/MC — вони збираються в
	// окрему чергу pendingResolve і оброблюються паралельно нижче.
	rows := make([]model.ModCompareRow, 0, len(keyOrder))
	type pendingResolve struct {
		rowIdx       int
		which        string // "left" | "right"
		mFrom        *model.ModEntry
		target       model.MigrationPack
		targetOK     bool
		sourcePackID string
	}
	var pending []pendingResolve

	for _, k := range keyOrder {
		lm := leftByKey[k]
		rm := rightByKey[k]

		row := model.ModCompareRow{Key: k}
		switch {
		case lm != nil:
			row.Name = lm.Name
			row.Author = lm.Author
			row.IconURL = firstNonEmpty(lm.IconData, lm.IconURL)
		case rm != nil:
			row.Name = rm.Name
			row.Author = rm.Author
			row.IconURL = firstNonEmpty(rm.IconData, rm.IconURL)
		}

		switch {
		case lm != nil && rm != nil:
			row.Left = buildSide(lm)
			row.Right = buildSide(rm)
			row.SameVersion = lm.Version != "" && lm.Version == rm.Version
			if row.SameVersion {
				report.BothSame++
			} else {
				report.Diff++
				// "Застаріла" сторона — та, чия версія не збігається;
				// не намагаємось вгадати, яка саме новіша (без семвер-
				// парсингу це ненадійно) — обидві сторони позначаються
				// outdated з можливістю "підняти" до версії іншої.
				row.Left.Status = "outdated"
				row.Left.ResolvedVersion = rm.Version
				row.Left.ResolvedFile = rm.FileName
				row.Left.CanInstall = true
				if sameEnv {
					row.Left.LocalCopy = true
					row.Left.SourcePackID = rightPackID
					row.Left.SourceFileName = rm.FileName
				}
				row.Right.Status = "outdated"
				row.Right.ResolvedVersion = lm.Version
				row.Right.ResolvedFile = lm.FileName
				row.Right.CanInstall = true
				if sameEnv {
					row.Right.LocalCopy = true
					row.Right.SourcePackID = leftPackID
					row.Right.SourceFileName = lm.FileName
				}
			}
		case lm != nil:
			row.Left = buildSide(lm)
			report.OnlyLeft++
			if sameEnv {
				// Пряме копіювання — без мережі, рахуємо одразу.
				row.Right = buildMissingSide(lm, rightPack, rightOK, leftPackID)
			} else {
				row.Right = model.ModCompareSide{Status: "missing"}
				if rightOK {
					rowIdx := len(rows)
					pending = append(pending, pendingResolve{rowIdx: rowIdx, which: "right", mFrom: lm, target: rightPack, targetOK: rightOK, sourcePackID: leftPackID})
				}
			}
		case rm != nil:
			row.Right = buildSide(rm)
			report.OnlyRight++
			if sameEnv {
				row.Left = buildMissingSide(rm, leftPack, leftOK, rightPackID)
			} else {
				row.Left = model.ModCompareSide{Status: "missing"}
				if leftOK {
					rowIdx := len(rows)
					pending = append(pending, pendingResolve{rowIdx: rowIdx, which: "left", mFrom: rm, target: leftPack, targetOK: leftOK, sourcePackID: rightPackID})
				}
			}
		default:
			continue
		}

		rows = append(rows, row)
	}

	// ── Крок 2: паралельний мережевий резолв відкладених сторін ─────────
	// Обмежений паралелізм (семафор на 6) — той самий принцип, що й у
	// AnalyzeModMigration: без нього 100+ модів з розбіжностями означали
	// б сотні ПОСЛІДОВНИХ мережевих round-trip'ів (кожен пошук за назвою
	// на Modrinth/CurseForge триває 200-800мс) — порівняння відчутно
	// "висло" на великих збірках. Тут кожен pending — незалежний резолв
	// (свій mFrom/target), тож паралелити безпечно; спільні лише
	// resolveCache (вже під mutex у resolve()) і сам зріз rows (пишемо
	// в різні rowIdx, але через мьютекс — про всяк випадок, бо append
	// вище міг реалокувати slice і індекси мають лишатись валідними
	// лише для читання/запису по фіксованому індексу, не append).
	if len(pending) > 0 {
		var rowsMu sync.Mutex
		sem := make(chan struct{}, 6)
		var wg sync.WaitGroup
		for _, p := range pending {
			p := p
			wg.Add(1)
			go func() {
				defer crashguard.Recover("mods_compare.go:408")
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				side := buildMissingSide(p.mFrom, p.target, p.targetOK, p.sourcePackID)
				rowsMu.Lock()
				if p.which == "left" {
					rows[p.rowIdx].Left = side
				} else {
					rows[p.rowIdx].Right = side
				}
				rowsMu.Unlock()
			}()
		}
		wg.Wait()
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return compareRowOrder(rows[i]) < compareRowOrder(rows[j]) ||
			(compareRowOrder(rows[i]) == compareRowOrder(rows[j]) && strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name))
	})
	report.Rows = rows
	return report, nil
}

// compareRowOrder — порядок сортування рядків: розбіжності і "тільки з
// одного боку" зверху (потребують уваги користувача), однакові — внизу.
func compareRowOrder(r model.ModCompareRow) int {
	switch {
	case r.Left.Status == "missing" || r.Right.Status == "missing":
		return 0
	case !r.SameVersion:
		return 1
	default:
		return 2
	}
}

// baseNameNoExtCompare — ім'я файлу без версійного/розширення хвоста,
// для фолбек-матчингу рядків, коли й modId, і sha1 порожні з обох боків
// (мінімальна гарантія — принаймні не показати два окремі рядки для
// візуально одного й того ж файлу).
func baseNameNoExtCompare(fileName string) string {
	name := strings.TrimSuffix(fileName, ".jar")
	name = strings.TrimSuffix(name, ".disabled")
	return name
}

// InstallCompareMod встановлює/оновлює ОДИН мод в ОДНУ конкретну сторону
// порівняння (кнопка на PackCompareCell). req.Side — те, що резолвнув
// CompareModPacks для цієї сторони (LocalCopy або Resolved*).
func (a *App) InstallCompareMod(req model.CompareInstallRequest) error {
	if req.TargetPackID == "" {
		return fmt.Errorf("не вказано цільову збірку")
	}
	side := req.Side
	if !side.LocalCopy && side.ResolvedURL == "" {
		return fmt.Errorf("немає що встановлювати для «%s»", req.SourceName)
	}

	targetDir := a.instanceModsDir(req.TargetPackID)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("не вдалось створити теку: %w", err)
	}

	backupsDir := a.modsBackupsDir(req.TargetPackID)
	stamp := ""
	if req.Replace {
		stamp = "compare-" + time.Now().Format("20060102-150405")
		os.MkdirAll(filepath.Join(backupsDir, stamp), 0755)
	}

	resolvedFilename := side.ResolvedFile
	if resolvedFilename == "" {
		resolvedFilename = side.FileName
	}

	// Прибираємо старі версії цього мода в цілі (за modId, fallback
	// projectId) — та сама логіка, що при міграції: інакше лишились би
	// дві версії одного мода одночасно.
	a.removeModFilesForMigration(targetDir, side.ModID, side.ProjectID)
	if resolvedFilename != "" {
		if req.Replace && stamp != "" {
			if old, err := os.ReadFile(filepath.Join(targetDir, resolvedFilename)); err == nil {
				os.WriteFile(filepath.Join(backupsDir, stamp, resolvedFilename), old, 0644)
			}
		}
		_ = os.Remove(filepath.Join(targetDir, resolvedFilename))
		_ = os.Remove(filepath.Join(targetDir, resolvedFilename+".disabled"))
	}

	tmp := filepath.Join(targetDir, "."+resolvedFilename+".part")

	if side.LocalCopy {
		if side.SourcePackID == "" || side.SourceFileName == "" {
			return fmt.Errorf("немає джерела для локального копіювання «%s»", req.SourceName)
		}
		srcPath := filepath.Join(a.instanceModsDir(side.SourcePackID), side.SourceFileName)
		src, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("не вдалося відкрити файл джерела: %w", err)
		}
		defer src.Close()
		dst, err := os.Create(tmp)
		if err != nil {
			return fmt.Errorf("не вдалося створити файл цілі: %w", err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			dst.Close()
			os.Remove(tmp)
			return fmt.Errorf("копіювання не вдалося: %w", err)
		}
		if err := dst.Close(); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, filepath.Join(targetDir, resolvedFilename)); err != nil {
			return err
		}
	} else {
		if err := a.downloadModFile(side.ResolvedURL, tmp); err != nil {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, filepath.Join(targetDir, resolvedFilename)); err != nil {
			return err
		}
	}

	if req.Replace && stamp != "" {
		if _, err := os.Stat(filepath.Join(backupsDir, stamp, resolvedFilename)); err == nil {
			a.appendBackupEntry(req.TargetPackID, model.ModEntry{
				ID: side.ModID, Name: req.SourceName, Version: side.ResolvedVersion, FileName: resolvedFilename,
			}, filepath.Join(stamp, resolvedFilename))
		}
	}

	a.invalidateModUpdateCache(req.TargetPackID)
	return nil
}
