package autorepair

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"shaurma-launcher-backend/internal/modgraph"
)

const (
	maxLogLines    = 1500
	maxEvidence    = 4
	maxFilesPerFix = 40
)

var libsPathRe = regexp.MustCompile(`(?:libraries|versions)[/\\]`)
var offlineVerifyRe = regexp.MustCompile(`(?i)Failed to verify username|Invalid session`)

// diagnoser — стан однієї діагностики.
type diagnoser struct {
	in     Input
	issues map[string]*Issue
}

// add реєструє проблему або доповнює вже знайдену того ж виду.
func (d *diagnoser) add(kind, detail string, fixes []*Fix, evidence []string, title, manual string) {
	live := make([]*Fix, 0, len(fixes))
	for _, f := range fixes {
		if f != nil {
			live = append(live, f)
		}
	}
	if len(live) == 0 && manual == "" {
		return
	}
	if existing, ok := d.issues[kind]; ok {
		// Повторна улика тієї ж природи — доповнюємо, а не дублюємо.
		existing.Evidence = head(unique(append(existing.Evidence, evidence...)), maxEvidence)
		if kind == "account" && detail != "" && !strings.Contains(existing.Detail, detail) {
			// У цієї проблеми причини незалежні — показуємо всі.
			existing.Detail = existing.Detail + " " + detail
			if manual != "" && !strings.Contains(existing.Manual, manual) {
				existing.Manual = strings.TrimSpace(existing.Manual + " " + manual)
			}
		}
		for _, fix := range live {
			var current *Fix
			for i := range existing.Fixes {
				ef := &existing.Fixes[i]
				if ef.Action == fix.Action && paramsEqual(ef.Params, fix.Params) {
					current = ef
					break
				}
			}
			if current == nil && (fix.Action == "disable_mods" || fix.Action == "install_dependencies") {
				for i := range existing.Fixes {
					if existing.Fixes[i].Action == fix.Action {
						current = &existing.Fixes[i]
						break
					}
				}
			}
			switch {
			case current == nil:
				fix.ID = fmt.Sprintf("%s:%s:%d", kind, fix.Action, len(existing.Fixes)+1)
				fix.Recommended = false
				existing.Fixes = append(existing.Fixes, *fix)
			case fix.Action == "disable_mods":
				files := head(unique(append(stringList(current.Params["files"]), stringList(fix.Params["files"])...)), maxFilesPerFix)
				current.Params["files"] = files
				current.Steps = disableSteps(files)
			case fix.Action == "install_dependencies":
				ids := head(unique(append(stringList(current.Params["ids"]), stringList(fix.Params["ids"])...)), maxFilesPerFix)
				current.Params["ids"] = ids
				current.Steps = installSteps(ids)
			}
		}
		return
	}
	used := map[string]bool{}
	fixList := make([]Fix, 0, len(live))
	for i, fix := range live {
		// У однієї проблеми може бути кілька лагоджень одного типу (наприклад,
		// «перемкнутись на акаунт A / B») — id має бути унікальним.
		id := kind + ":" + fix.Action
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s:%s:%d", kind, fix.Action, n)
		}
		used[id] = true
		fix.ID = id
		fix.Recommended = i == 0
		fixList = append(fixList, *fix)
	}
	if title == "" {
		title = kindTitle(kind)
	}
	d.issues[kind] = &Issue{
		ID: kind, Title: title, Detail: detail,
		Evidence: head(unique(evidence), maxEvidence), Fixes: fixList, Manual: manual,
	}
}

// Diagnose аналізує запуск і повертає проблеми зі способами лагодження.
func Diagnose(in Input) Report {
	loader := strings.ToLower(strings.TrimSpace(in.Loader))
	if loader == "" {
		loader = "vanilla"
	}
	loaderTitle := loaderTitles[loader]
	if loaderTitle == "" {
		loaderTitle = strings.ToUpper(loader[:1]) + loader[1:]
	}
	loaderIDs := loaderDependencyIDs[loader]

	logLines := in.Lines
	if len(logLines) > maxLogLines {
		logLines = logLines[len(logLines)-maxLogLines:]
	}
	logLines = append([]string(nil), logLines...)
	if in.Error != "" {
		logLines = append(logLines, in.Error)
	}
	var report string
	if in.CrashReport != nil {
		report = *in.CrashReport
	} else {
		report = latestCrashReport(in.GameDir, in.Since)
	}
	text := strings.Join(logLines, "\n")
	combined := text
	if report != "" {
		combined = text + "\n" + report
	}

	var mods []modgraph.ModFile
	if loader != "vanilla" && in.GameDir != "" {
		mods, _ = modgraph.Scan(modsDirOf(in.GameDir))
	}
	var enabled []modgraph.ModFile
	for _, m := range mods {
		if m.Enabled {
			enabled = append(enabled, m)
		}
	}
	byID := map[string]modgraph.ModFile{}
	for _, m := range enabled {
		if m.ModID != "" {
			byID[m.ModID] = m
		}
	}
	d := &diagnoser{in: in, issues: map[string]*Issue{}}

	// --- Залежності та сумісність модів --------------------------------------
	missing := map[string]map[string]bool{}
	addMissing := func(dep, requester string) {
		if missing[dep] == nil {
			missing[dep] = map[string]bool{}
		}
		missing[dep][requester] = true
	}
	type wrongVer struct{ requester, dependency, line string }
	var wrongVersion []wrongVer
	var wrongGame []string
	javaRequired := 0
	type loaderReq struct{ requester, minimum, line string }
	var loaderReqs []loaderReq

	for _, m := range fabricRequires.FindAllStringSubmatch(combined, -1) {
		requester, dep := strings.ToLower(m[2]), dependencyID(m)
		if dep == "java" || allLoaderDependencyIDs[dep] {
			continue // чужий завантажувач ловить перевірка wrong_loader_mods
		}
		if dep == "minecraft" {
			wrongGame = append(wrongGame, requester)
			continue
		}
		addMissing(dep, requester)
	}
	for _, m := range fabricWrongVersion.FindAllStringSubmatch(combined, -1) {
		requester, dep := strings.ToLower(m[2]), dependencyID(m)
		switch {
		case dep == "java":
			javaRequired = maxInt(javaRequired, firstInt(m[3]))
		case dep == "minecraft":
			wrongGame = append(wrongGame, requester)
		case loaderIDs[dep]:
			loaderReqs = append(loaderReqs, loaderReq{requester, firstVersion(m[3]), strings.TrimSpace(m[0])})
		default:
			wrongVersion = append(wrongVersion, wrongVer{requester, dep, strings.TrimSpace(m[0])})
		}
	}
	for _, m := range forgeDependency.FindAllStringSubmatch(combined, -1) {
		dep, requester, actual := strings.ToLower(m[1]), strings.ToLower(m[2]), m[4]
		switch {
		case dep == "java":
			javaRequired = maxInt(javaRequired, firstInt(m[3]))
		case dep == "minecraft":
			wrongGame = append(wrongGame, requester)
		case loaderIDs[dep]:
			loaderReqs = append(loaderReqs, loaderReq{requester, firstVersion(m[3]), strings.TrimSpace(m[0])})
		case strings.Contains(strings.ToLower(actual), "missing") || strings.TrimSpace(actual) == "":
			addMissing(dep, requester)
		default:
			wrongVersion = append(wrongVersion, wrongVer{requester, dep, strings.TrimSpace(m[0])})
		}
	}
	for _, m := range fmlRequires.FindAllStringSubmatch(combined, -1) {
		requester, dep, minimum, state := strings.ToLower(m[1]), strings.ToLower(m[2]), m[3], m[5]
		switch {
		case dep == "minecraft":
			wrongGame = append(wrongGame, requester)
		case loaderIDs[dep]:
			loaderReqs = append(loaderReqs, loaderReq{requester, firstVersion(minimum), strings.TrimSpace(m[0])})
		case dep == "java" || allLoaderDependencyIDs[dep]:
		case strings.EqualFold(state, "not installed"):
			addMissing(dep, requester)
		default:
			wrongVersion = append(wrongVersion, wrongVer{requester, dep, strings.TrimSpace(m[0])})
		}
	}
	// Статичний аналіз: обов'язкові залежності, яких немає в теці mods.
	index := modgraph.BuildIndex(mods, true)
	for _, mod := range enabled {
		if mod.Corrupted {
			continue
		}
		for _, dep := range mod.Dependencies {
			id := strings.ToLower(strings.TrimSpace(dep.ID))
			if id == "" || dep.Optional || modgraph.IsNoise(id) || libraryDependencyIDs[id] || allLoaderDependencyIDs[id] {
				continue
			}
			if _, ok := index.Lookup(id); ok {
				continue
			}
			// підмодулі Fabric/Quilt API лежать jar-in-jar усередині самого API
			if (strings.HasPrefix(id, "fabric-") || strings.HasPrefix(id, "fabric_")) && id != "fabric-api" {
				continue
			}
			if strings.HasPrefix(id, "quilt_") || strings.HasPrefix(id, "quilted_") || strings.HasPrefix(id, "forge") ||
				strings.HasPrefix(id, "neoforge") || strings.HasPrefix(id, "java") {
				continue
			}
			// статикою ловимо лише те, що лог теж назвав, або коли логу немає зовсім
			if _, named := missing[id]; named || strings.TrimSpace(combined) == "" {
				addMissing(id, mod.ModID)
			}
		}
	}
	if len(missing) > 0 {
		names := sortedKeys(missing)
		requesterSet := map[string]bool{}
		for _, s := range missing {
			for r := range s {
				requesterSet[r] = true
			}
		}
		requesters := sortedKeys(requesterSet)
		detail := "Модам бракує обов'язкових бібліотек: " + strings.Join(head(names, 8), ", ")
		if len(requesters) > 0 {
			detail += fmt.Sprintf(" (потрібні для: %s)", strings.Join(head(requesters, 6), ", "))
		}
		d.add("missing_dependencies", detail+".", []*Fix{
			installFix(names),
			disableFix(filesForIDs(requesters, byID), "Вимкнути моди, яким бракує залежностей"),
		}, evidenceOf(combined, append(append([]string{}, names...), "missing")), "", "")
	}
	if len(loaderReqs) > 0 && loaderIDs != nil {
		minimum := ""
		var reqNames, lines []string
		for _, r := range loaderReqs {
			if r.minimum != "" && (minimum == "" || cmpVersion(r.minimum, minimum) > 0) {
				minimum = r.minimum
			}
			reqNames = append(reqNames, r.requester)
			lines = append(lines, r.line)
		}
		reqNames = unique(reqNames)
		need := "новіший"
		if minimum != "" {
			need = minimum + " або новіший"
		}
		detail := fmt.Sprintf("Модам потрібен %s %s", loaderTitle, need)
		if in.LoaderVersion != "" {
			detail += ", а встановлено " + in.LoaderVersion
		}
		detail += fmt.Sprintf(" (вимагають: %s).", strings.Join(head(reqNames, 6), ", "))
		manual := ""
		if !in.CanUpdateLoader {
			manual = "Версію завантажувача в цій збірці задає її автор — оновіть збірку або вимкніть моди, яким потрібен новіший " + loaderTitle + "."
		}
		d.add("loader_outdated", detail, []*Fix{
			updateLoaderFix(in, loader, minimum, false),
			disableFix(filesForIDs(reqNames, byID), "Вимкнути моди, яким потрібен новіший "+loaderTitle),
		}, lines, "", manual)
	}
	if len(wrongVersion) > 0 {
		var reqs, lines, pairs []string
		for _, w := range wrongVersion {
			reqs = append(reqs, w.requester)
			lines = append(lines, w.line)
			pairs = append(pairs, w.requester+" → "+w.dependency)
		}
		d.add("incompatible_mods", "Моди вимагають іншу версію залежності: "+strings.Join(head(pairs, 5), "; ")+".",
			[]*Fix{disableFix(filesForIDs(reqs, byID), "Вимкнути моди з невідповідними версіями залежностей")}, lines, "", "")
	}
	if len(wrongGame) > 0 {
		detail := "Частину модів зібрано для іншої версії Minecraft"
		if in.MinecraftVersion != "" {
			detail += fmt.Sprintf(" (у збірки %s)", in.MinecraftVersion)
		}
		d.add("incompatible_mods", detail+".",
			[]*Fix{disableFix(filesForIDs(wrongGame, byID), "Вимкнути моди для іншої версії Minecraft")},
			evidenceOf(combined, []string{"minecraft"}), "", "")
	}
	var incompatFiles, incompatLines []string
	for _, m := range fabricIncompatible.FindAllStringSubmatch(combined, -1) {
		incompatFiles = append(incompatFiles, filesForIDs([]string{strings.ToLower(m[2])}, byID)...)
		incompatLines = append(incompatLines, strings.TrimSpace(m[0]))
	}
	for _, pat := range []*regexp.Regexp{moduleConflict, moduleReads} {
		for _, m := range pat.FindAllStringSubmatch(combined, -1) {
			incompatFiles = append(incompatFiles, filesForNames([]string{m[1], m[2]}, enabled)...)
			incompatLines = append(incompatLines, strings.TrimSpace(m[0]))
		}
	}
	if len(incompatLines) > 0 {
		d.add("incompatible_mods", "Завантажувач повідомив про конфлікт модів між собою.",
			[]*Fix{disableFix(unique(incompatFiles), "Вимкнути конфліктуючі моди")}, incompatLines, "", "")
	}
	if incompatibleSet.MatchString(combined) && d.issues["incompatible_mods"] == nil && d.issues["missing_dependencies"] == nil {
		suspects := crashReportSuspects(report, enabled)
		d.add("incompatible_mods", "Завантажувач не зміг зібрати набір модів, але не назвав конкретного винуватця.",
			[]*Fix{disableFix(suspects, "Вимкнути підозрювані моди"), recentFix(enabled, in)},
			evidenceOf(combined, []string{"incompatible", "resolution"}), "", "")
	}

	// --- Моди для іншого завантажувача ----------------------------------------
	var wrongLoader []string
	for _, mod := range enabled {
		if isWrongLoader(mod, loader, byID) {
			wrongLoader = append(wrongLoader, mod.File)
		}
	}
	if len(wrongLoader) > 0 {
		d.add("wrong_loader_mods", fmt.Sprintf("У теці mods є моди, розраховані не на %s: вони не завантажаться або обваляють гру.", loaderTitle),
			[]*Fix{disableFix(wrongLoader, "Вимкнути моди не для "+loaderTitle)}, head(wrongLoader, maxEvidence), "", "")
	}

	// --- Дублікати ---------------------------------------------------------------
	seen := map[string][]modgraph.ModFile{}
	var seenOrder []string
	for _, mod := range enabled {
		if mod.ModID != "" {
			if _, ok := seen[mod.ModID]; !ok {
				seenOrder = append(seenOrder, mod.ModID)
			}
			seen[mod.ModID] = append(seen[mod.ModID], mod)
		}
	}
	var duplicates, duplicateLines []string
	for _, id := range seenOrder {
		group := seen[id]
		if len(group) < 2 {
			continue
		}
		// лишаємо найсвіжіший файл, решту вимикаємо
		sort.SliceStable(group, func(i, j int) bool { return group[i].MTime > group[j].MTime })
		files := make([]string, 0, len(group))
		for i, g := range group {
			files = append(files, g.File)
			if i > 0 {
				duplicates = append(duplicates, g.File)
			}
		}
		duplicateLines = append(duplicateLines, id+": "+strings.Join(files, ", "))
	}
	for _, m := range forgeDuplicate.FindAllStringSubmatch(combined, -1) {
		duplicateLines = append(duplicateLines, strings.TrimSpace(m[0]))
	}
	if len(duplicates) > 0 {
		d.add("duplicate_mods", "Той самий мод лежить у теці mods кілька разів (різні версії).",
			[]*Fix{disableFix(duplicates, "Лишити найновішу копію, решту вимкнути")}, duplicateLines, "", "")
	}

	// --- Mixin ------------------------------------------------------------------
	var mixinMods []string
	for _, pat := range []*regexp.Regexp{mixinForMod, mixinFromMod} {
		for _, m := range pat.FindAllStringSubmatch(combined, -1) {
			mixinMods = append(mixinMods, strings.ToLower(m[1]))
		}
	}
	mixinFiles := filesForIDs(mixinMods, byID)
	if mixinWord.MatchString(combined) && mixinKind.MatchString(combined) {
		configs := map[string]bool{}
		for _, m := range mixinConfig.FindAllStringSubmatch(combined, -1) {
			configs[strings.ToLower(m[1])] = true
		}
		for _, mod := range enabled {
			for _, c := range mod.MixinConfigs {
				if configs[c] {
					mixinFiles = append(mixinFiles, mod.File)
					break
				}
			}
		}
		mixinFiles = unique(mixinFiles)
		detail := "Мод не зміг упровадити свій код у гру (Mixin). Зазвичай це мод для іншої версії гри або конфлікт з іншим модом."
		if len(mixinFiles) > 0 {
			detail += fmt.Sprintf(" Винуватець: %s.", strings.Join(head(mixinFiles, 4), ", "))
		}
		d.add("mixin_failure", detail,
			[]*Fix{disableFix(mixinFiles, "Вимкнути мод з помилкою Mixin"), recentFix(enabled, in)},
			evidenceOf(combined, []string{"mixin"}), "", "")
	}

	// --- Пошкоджені jar ------------------------------------------------------------
	var corrupted []string
	for _, mod := range enabled {
		if mod.Corrupted {
			corrupted = append(corrupted, mod.File)
		}
	}
	if len(corrupted) > 0 {
		ev := append(append([]string{}, head(corrupted, 2)...), evidenceOf(combined, []string{"zip"})...)
		d.add("corrupted_mods", "Частина jar-файлів у теці mods не відкривається як архів — імовірно, завантаження перервалося.",
			[]*Fix{disableFix(corrupted, "Вимкнути пошкоджені файли (потім завантажте їх заново)")}, ev, "", "")
	}

	// --- Конфіги ----------------------------------------------------------------------
	if cfgFiles := brokenConfigs(combined, in.GameDir); len(cfgFiles) > 0 && configParseErr.MatchString(combined) {
		d.add("broken_config", "Мод не зміг прочитати свій конфіг. Після перенесення файлу мод створить налаштування за замовчуванням.",
			[]*Fix{configFix(cfgFiles)}, evidenceOf(combined, []string{"config"}), "", "")
	}

	// --- Пам'ять --------------------------------------------------------------------------
	currentMB := in.EffectiveRAMMB
	recommended := in.RecommendedRAMMB
	if recommended <= 0 {
		recommended = 4096
	}
	if memoryTooHigh.MatchString(combined) {
		target := maxInt(1024, minInt(recommended, currentMB/2))
		target = (target / 512) * 512
		if target == 0 {
			target = 1024
		}
		if target < currentMB {
			d.add("memory_too_high", fmt.Sprintf("Java не змогла зарезервувати %s — системі бракує вільної пам'яті.", humanMemory(currentMB)),
				[]*Fix{memoryFix(currentMB, target, "Зменшити пам'ять збірки")},
				evidenceOf(combined, []string{"memory", "heap", "reserve"}), "", "")
		}
	} else if oom.MatchString(combined) {
		ceiling := 8192
		if in.TotalRAMMB > 0 {
			ceiling = in.TotalRAMMB * 3 / 4
		}
		target := minInt(ceiling, maxInt(currentMB+2048, maxInt(currentMB*3/2, 4096)))
		target = (target / 512) * 512
		if target > currentMB {
			d.add("out_of_memory", fmt.Sprintf("Грі не вистачило виділеної пам'яті (%s). Важким збіркам зазвичай потрібно 4–8 ГБ.", humanMemory(currentMB)),
				[]*Fix{memoryFix(currentMB, target, "Збільшити пам'ять збірки")},
				evidenceOf(combined, []string{"OutOfMemory", "heap"}), "", "")
		}
	}

	// --- Java ------------------------------------------------------------------------------------
	if strings.Contains(combined, "UnsupportedClassVersionError") || strings.Contains(combined, "more recent version of the Java") {
		for _, m := range classFileVersion.FindAllStringSubmatch(combined, -1) {
			if v, err := strconv.Atoi(m[1]); err == nil {
				javaRequired = maxInt(javaRequired, v-44)
			}
		}
	}
	for _, m := range requiresJava.FindAllStringSubmatch(combined, -1) {
		javaRequired = maxInt(javaRequired, firstInt(m[1]+m[2]+m[3]+m[4]))
	}
	switch {
	case javaRequired >= 8 || strings.Contains(combined, "UnsupportedClassVersionError"):
		need := "новіша"
		if javaRequired > 0 {
			need = strconv.Itoa(javaRequired)
		}
		d.add("java_version", fmt.Sprintf("Грі або модам потрібна Java %s, а запуск ішов на старішій.", need),
			javaFixes(javaRequired, in.JavaPath), evidenceOf(combined, []string{"java", "class file version"}), "",
			fmt.Sprintf("Лаунчер сам завантажить Java %s. Якщо у збірці вказано власну Java — поверніть автовибір.", need))
	case javaMissing.MatchString(combined):
		d.add("java_version", "Лаунчер не знайшов придатну Java для запуску.", javaFixes(0, in.JavaPath),
			evidenceOf(combined, []string{"java"}), "Java не знайдено", "")
	}

	// --- JVM ------------------------------------------------------------------------------------------
	if jvmArguments.MatchString(combined) && d.issues["memory_too_high"] == nil {
		var fixes []*Fix
		if in.CustomJVM {
			fixes = append(fixes, jvmResetFix(in))
		} else if heapSizeWord.MatchString(combined) {
			fixes = append(fixes, memoryFix(currentMB, recommended, "Скинути пам'ять на рекомендовану"))
		}
		d.add("jvm_arguments", "Java відмовилась стартувати через параметри запуску (аргументи JVM).",
			fixes, evidenceOf(combined, []string{"VM option", "Virtual Machine", "option"}), "", "")
	}
	// Крах JVM через відсутній GL-контекст (26.x: LWJGL абортить JVM, коли
	// графічного бекенду немає) — це не «винна Java»: інакше користувач
	// отримував би другу, хибну пораду поруч із graphics.
	if jvmCrash.MatchString(combined) && !graphicsNoAdapter.MatchString(combined) {
		driver := driverFrame.MatchString(combined)
		var fixes []*Fix
		if driver {
			fixes = append(fixes, graphicsFixes(in, enabled)...)
		}
		if in.CustomJVM {
			fixes = append(fixes, jvmResetFix(in))
		}
		if in.JavaPath != "" {
			// Для краху JVM іншу версію навмання не підбираємо: старим версіям гри
			// потрібна саме їхня Java. Автовибір візьме ту, що вимагає версія.
			fixes = append(fixes, autoJavaFix(in.JavaPath))
		}
		detail := "Впала сама Java, а не Minecraft. Часті причини: власні аргументи JVM, збійна збірка Java або драйвер."
		manual := ""
		if driver {
			detail = "Впала сама Java, а не Minecraft — збій усередині графічного драйвера."
			manual = "Якщо крах повторюється, оновіть драйвер відеокарти та Java."
		}
		d.add("jvm_crash", detail, fixes, evidenceOf(combined, []string{"fatal error", "SIGSEGV", "ACCESS_VIOLATION", "hs_err"}), "", manual)
	}

	// --- Модлоадер ----------------------------------------------------------------------------------------------
	if loaderIDs != nil && loaderBroken.MatchString(combined) {
		detail := fmt.Sprintf("%s не зміг стартувати: файли завантажувача відсутні, пошкоджені або встановлені не до кінця", loaderTitle)
		if in.LoaderVersion != "" {
			detail += fmt.Sprintf(" (версія %s)", in.LoaderVersion)
		}
		manual := ""
		if nonLatinPath.MatchString(combined) {
			manual = "У шляху до гри є не-латинські символи. Якщо перевстановлення не допоможе, оновіть лаунчер або перенесіть теку гри в шлях латиницею (наприклад, C:\\Games\\Shaurma)."
		}
		d.add("loader_broken", detail+".", []*Fix{repairFix(loader), updateLoaderFix(in, loader, "", true)},
			evidenceOf(combined, []string{"main class", "ClassNotFound", "NoClassDefFound", "installer", "jar", "mainClass", "FindException", "system mod", "metadata"}), "", manual)
	}

	// --- Акаунт -----------------------------------------------------------------------------------------------------------
	d.diagnoseAccount(combined)

	// --- Файли гри та завантажувача -----------------------------------------------------------------------------------------
	zipInLibs := corruptedZip.MatchString(combined) && libsPathRe.MatchString(combined)
	if (gameFiles.MatchString(combined) || zipInLibs) && d.issues["loader_broken"] == nil {
		d.add("game_files", "Не вистачає або пошкоджено файли версії, бібліотек чи модлоадера.",
			[]*Fix{repairFix(loader)},
			evidenceOf(combined, []string{"ClassNotFound", "main class", "lwjgl", "libraries", "download", "mismatch", "jar"}), "", "")
	}

	// --- Графіка ----------------------------------------------------------------------------------------------------------------
	if graphics.MatchString(combined) {
		detail := "Minecraft не зміг створити вікно з OpenGL. Зазвичай допомагає оновити драйвер відеокарти; лаунчер може спробувати обхідні шляхи."
		manual := "Встановіть свіжий драйвер відеокарти з сайту NVIDIA/AMD/Intel; на ноутбуці переконайтеся, що гра запускається на дискретній відеокарті."
		needles := []string{"OpenGL", "GLFW", "GL", "window"}
		if graphicsNoAdapter.MatchString(combined) {
			detail = "Гра не змогла створити графічний контекст: відеокарта не дала жодного придатного адаптера ні для OpenGL, ні для Vulkan. Вікно не створилося, а LWJGL аварійно зупинив JVM — тому звіт про крах не пишеться."
			manual = "Що робити: 1) оновіть драйвер відеокарти з сайту NVIDIA/AMD/Intel (чиста установка) і перезавантажте ПК; 2) якщо в лаунчері ввімкнено «Дискретна GPU для гри» — вимкніть її та перезапустіть лаунчер; 3) dxdiag → вкладка Display: «Microsoft Basic Display Adapter» означає, що драйвера немає; 4) не запускайте гру через RDP/віддалений робочий стіл."
			needles = []string{"graphics adapter", "No context is current", "Vulkan", "OpenGL", "window"}
		}
		d.add("graphics", detail, graphicsFixes(in, enabled), evidenceOf(combined, needles), "", manual)
	}

	// --- Запасні варіанти ----------------------------------------------------------------------------------------------------------------
	if len(d.issues) == 0 && ((in.ExitCode != nil && *in.ExitCode != 0) || in.Error != "") {
		if suspects := crashReportSuspects(report, enabled); len(suspects) > 0 {
			d.add("incompatible_mods", "Звіт про крах вказує на конкретні моди.",
				[]*Fix{disableFix(suspects, "Вимкнути моди зі звіту про крах")},
				evidenceOf(report, []string{"Suspected", "Mod File"}), "Крах спричинив мод", "")
		}
		if recent := recentFix(enabled, in); recent != nil {
			d.add("recent_mods", "Явної причини в логах немає, але після останнього успішного запуску з'явилися нові моди — часте джерело крахів.",
				[]*Fix{recent}, head(stringList(recent.Params["files"]), maxEvidence), "", "")
		}
		if len(d.issues) == 0 {
			d.add("unknown_crash", "Явних ознак у логах не знайшлося. Можна перевірити й відновити файли версії та завантажувача — моди, світи й налаштування не чіпаються.",
				[]*Fix{repairFix(loader)}, tailEvidence(logLines), "", "")
		}
	}

	mutedSet := set(in.Muted...)
	rep := Report{ExitCode: in.ExitCode, Issues: []Issue{}, MutedIssues: []string{}}
	for _, kind := range issueKinds {
		issue, ok := d.issues[kind.ID]
		if !ok {
			continue
		}
		if mutedSet[issue.ID] {
			rep.MutedIssues = append(rep.MutedIssues, issue.ID)
		} else {
			rep.Issues = append(rep.Issues, *issue)
		}
	}
	return rep
}

// ── акаунти ───────────────────────────────────────────────────────────────

func accountLabel(a Account) string {
	name := a.Name
	if name == "" {
		name = a.ID
	}
	if name == "" {
		name = "акаунт"
	}
	kind := "офлайн"
	if a.Licensed {
		kind = "Microsoft"
	}
	return fmt.Sprintf("%s (%s)", name, kind)
}

func (d *diagnoser) diagnoseAccount(combined string) {
	var known []Account
	for _, a := range d.in.Accounts {
		if a.ID != "" {
			known = append(known, a)
		}
	}
	currentID := d.in.CurrentAccountID
	var current *Account
	for i := range known {
		if known[i].ID == currentID {
			current = &known[i]
		}
	}
	if current == nil && currentID == "" {
		for i := range known {
			if known[i].Active {
				current = &known[i]
			}
		}
	}
	cid := ""
	if current != nil {
		cid = current.ID
	}
	var others, msOthers []Account
	for _, a := range known {
		if a.ID != cid {
			others = append(others, a)
			if a.Licensed {
				msOthers = append(msOthers, a)
			}
		}
	}
	setFixes := func(list []Account, reason string) []*Fix {
		var out []*Fix
		for _, a := range head2(list, 2) {
			out = append(out, setAccountFix(a, reason))
		}
		return out
	}

	if m := accountXSTS.FindStringSubmatch(combined); m != nil {
		code := m[1]
		hint := xstsHints[code]
		switch {
		case m[2] != "":
			hint = xstsHints["2148916233"]
		case m[3] != "":
			hint = xstsHints["2148916238"]
		}
		detail := "Xbox Live відхилив вхід"
		if code != "" {
			detail += fmt.Sprintf(" (код %s)", code)
		}
		if hint == "" {
			hint = "Перевірте акаунт на xbox.com і повторіть вхід."
		}
		d.add("account", detail+".", append(setFixes(msOthers, ""), loginFix("Увійти іншим акаунтом Microsoft")),
			evidenceOf(combined, []string{"XSTS", "Xbox"}), "Xbox Live не пустив акаунт", hint)
	}
	if accountNoLicense.MatchString(combined) {
		d.add("account", "В акаунта Microsoft не знайдено ліцензію Minecraft: Java Edition.",
			append(setFixes(msOthers, ""), loginFix("Увійти акаунтом, на якому куплено гру")),
			evidenceOf(combined, []string{"own", "profile", "Minecraft Services"}), "Немає ліцензії Minecraft",
			"Гру потрібно купити на minecraft.net тим самим акаунтом Microsoft. Game Pass дає ліцензію, лише поки підписка активна.")
	}
	if accountMissing.MatchString(combined) {
		var fixes []*Fix
		var fallback *Account
		for i := range others {
			if others[i].Licensed {
				fallback = &others[i]
				break
			}
		}
		if fallback == nil && len(others) > 0 {
			fallback = &others[0]
		}
		if fallback != nil {
			fixes = append(fixes, setAccountFix(*fallback, "Обраний акаунт видалено або недоступний — збірка отримає робочий."))
		}
		fixes = append(fixes, loginFix("Увійти через Microsoft"))
		d.add("account", "Акаунт, обраний для запуску, не знайдено: його видалили або ще не створено.", fixes,
			evidenceOf(combined, []string{"акаунт", "account"}), "Акаунт для запуску не знайдено", "")
	}
	storage := accountStorage.MatchString(combined)
	if accountRelogin.MatchString(combined) || storage {
		offlineOnServer := current != nil && !current.Licensed && offlineVerifyRe.MatchString(combined)
		var fixes []*Fix
		if offlineOnServer {
			fixes = append(fixes, setFixes(msOthers, "Онлайн-сервери пускають лише ліцензійні акаунти.")...)
			fixes = append(fixes, loginFix("Увійти через Microsoft"))
		} else {
			fixes = append(fixes, loginFix(""))
			fixes = append(fixes, setFixes(others, "")...)
		}
		manual := ""
		switch {
		case storage:
			manual = "Токени зберігаються в системному сховищі паролів (Credential Manager / gnome-keyring / KWallet). Якщо воно не запущене або заблоковане, вхід доведеться повторювати — перевірте, що сховище працює."
		case offlineOnServer:
			manual = "Офлайн-акаунт підходить для одиночної гри та серверів з online-mode=false."
		}
		detail, title := "Сесія Microsoft застаріла або токени втрачено — Minecraft не може підтвердити акаунт.", "Потрібно увійти в акаунт заново"
		if offlineOnServer {
			detail, title = "Сервер відхилив офлайн-акаунт: для цього сервера потрібна ліцензія.", "Офлайн-акаунт не пускають на сервер"
		}
		d.add("account", detail, fixes, evidenceOf(combined, []string{"token", "токен", "session", "verify", "login", "401", "сховищ", "secret"}), title, manual)
	}
	if accountMulti.MatchString(combined) {
		d.add("account", "Мережеву гру для акаунта вимкнено.", setFixes(msOthers, ""),
			evidenceOf(combined, []string{"multiplayer", "banned"}), "Мережева гра заборонена для акаунта",
			"Дозвольте мережеву гру в налаштуваннях конфіденційності Xbox (account.xbox.com → Конфіденційність) або в налаштуваннях сім'ї Microsoft.")
	}
}

func head2(list []Account, n int) []Account {
	if len(list) > n {
		return list[:n]
	}
	return list
}
