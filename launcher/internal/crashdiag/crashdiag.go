// Package crashdiag — локальний детектор причин крашу гри (БЕЗ мережі,
// без Gemini). Порт CrashDiagnostics.java зі старого JavaFX-лаунчера:
// правила розпізнавання лежать у crash-patterns.json (вбудований ресурс +
// користувацький файл-оверрайд у теці конфігурації лаунчера), тому нові
// типи помилок можна додавати без перекомпіляції.
//
// Аналіз працює ВИКЛЮЧНО над текстом логу, який вже є в буфері консолі
// (без мережі, без важкого I/O, окрім читання crash-report файлу, якщо
// рядок-маркер у логу на нього вказує).
package crashdiag

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

//go:embed crash-patterns.json
var bundledRulesJSON []byte

// Kind — тип розпізнаної проблеми.
type Kind string

const (
	KindMissingDependency Kind = "MISSING_DEPENDENCY" // мод вимагає іншу залежність, якої немає
	KindModConflict       Kind = "MOD_CONFLICT"       // два моди конфліктують між собою
	KindSingleModError    Kind = "SINGLE_MOD_ERROR"   // помилка локалізована в одному моді
	KindEnvCorruption     Kind = "ENV_CORRUPTION"     // пошкоджені файли оточення: Java/нативки/асети/jar
	KindUnknown           Kind = "UNKNOWN"            // не вдалось розпізнати
)

// LoaderScope — лоадер, до якого застосовується правило. ANY — завжди.
type LoaderScope string

const (
	LoaderFabric   LoaderScope = "fabric"
	LoaderForge    LoaderScope = "forge"
	LoaderNeoForge LoaderScope = "neoforge"
	LoaderQuilt    LoaderScope = "quilt"
	LoaderVanilla  LoaderScope = "vanilla"
	LoaderAny      LoaderScope = "any"
)

// Diagnosis — результат аналізу логу.
type Diagnosis struct {
	Kind               Kind
	Summary            string   // короткий людський опис ("суть помилки")
	RawExcerpt         string   // компактний уривок з лога, з якого зроблено висновок
	MissingDepName     string   // назва відсутньої залежності (MISSING_DEPENDENCY)
	ConflictingMods    []string // імена файлів модів-кандидатів на конфлікт
	CulpritModFilename string   // .jar файл, ймовірно винний (якщо вдалось встановити)
	CrashReportFile    string   // шлях до повного crash-report файлу, якщо знайдено
	// RepairSuggested — ознака, що проблему можна виправити кнопкою
	// «Полагодити» (пошкоджені/відсутні файли оточення: Java, нативки,
	// асети, бібліотеки). Frontend показує кнопку + пояснення, де вона
	// знаходиться (розділ «Продуктивність» на сторінці збірки) або діє
	// прямо з консолі.
	RepairSuggested bool
}

// Rule — одне правило розпізнавання з crash-patterns.json.
type Rule struct {
	ID             string
	Loader         LoaderScope
	Kind           Kind
	Pattern        *regexp.Regexp
	RequesterGroup int
	MissingGroup   int
	ConflictGroups []int
	Summary        string
}

var (
	cachedRules     []Rule
	cachedRulesOnce sync.Once

	// Резервні (страхувальні) патерни — для детекторів, які не вписуються у
	// простий формат "група → підстановка" (міксин-конфлікт, помилка одного
	// мода за стек-трейсом).
	fallbackExceptionHeader = regexp.MustCompile(`(?im)^.*?((?:[a-zA-Z][a-zA-Z0-9]*\.)+[A-Za-z0-9$]*Exception|[A-Za-z0-9$]*Error)\b.*$`)
	stacktraceJar          = regexp.MustCompile(`\(([A-Za-z0-9_.\-]+\.jar)[^)]*\)|\[([A-Za-z0-9_.\-]+\.jar)[^\]]*]`)
	mixinApplyError        = regexp.MustCompile(`(?is)mixin\s+apply\s+failed.{0,200}?\btarget\b.{0,120}`)

	// Minecraft не пише повний crash-report у stdout/stderr — лише цей
	// рядок-вказівник на файл, куди реально збережено стек-трейс.
	crashReportLocation = regexp.MustCompile(`#@!@# Game crashed! Crash report saved to: #@!@#\s*(?P<path>.+)`)
	crashReportFallback = regexp.MustCompile(`(?i)crash report saved to:?\s*(?P<path>[^\r\n]+\.txt)`)

	exceptionTypePattern = regexp.MustCompile(`((?:[a-zA-Z][a-zA-Z0-9]*\.)+[A-Za-z0-9$]*Exception|[A-Za-z0-9$]*Error)`)
	templatePlaceholder  = regexp.MustCompile(`\{(\d+)}`)
)

// Analyze — головний вхід: аналізує накопичений лог (останні рядки важливіші)
// і повертає діагноз. userRulesPath — опційний шлях до користувацького
// crash-patterns.json ("" = лише вбудований). modsDir — тека модів збірки,
// щоб зіставляти імена модів з файлами .jar.
func Analyze(recentLines []string, modsDir string, loader LoaderScope, userRulesPath string) *Diagnosis {
	if len(recentLines) == 0 {
		return nil
	}
	joined := strings.Join(recentLines, "\n")

	// Якщо в логу є рядок-вказівник на crash-report — читаємо повний файл
	// і аналізуємо його вміст (там справжній стек-трейс).
	crashReportFile, reportContent := readCrashReportIfReferenced(joined)
	if reportContent != "" {
		joined = joined + "\n" + reportContent
	}

	rules := LoadRules(userRulesPath)

	if d := applyRules(rules, KindMissingDependency, joined, modsDir, loader); d != nil {
		return attachReport(d, crashReportFile)
	}
	if d := applyRules(rules, KindModConflict, joined, modsDir, loader); d != nil {
		return attachReport(d, crashReportFile)
	}
	if d := detectMixinConflict(joined); d != nil {
		return attachReport(d, crashReportFile)
	}
	if d := detectSingleModError(joined, modsDir); d != nil {
		return attachReport(d, crashReportFile)
	}
	// Пошкодження файлів оточення (Java/нативки/асети/jar) — після модних
	// помилок, але перед "невідомо": це найчастіша причина крашів одразу
	// після переміщення/очищення тек збірки, і на неї є чіткий ремонт
	// («Полагодити»).
	if d := detectEnvCorruption(joined); d != nil {
		return attachReport(d, crashReportFile)
	}

	// Жодне правило не спрацювало, але файл крашу знайдено — краще показати
	// "невідомо, але ось файл", ніж нічого.
	if crashReportFile != "" {
		return &Diagnosis{
			Kind:            KindUnknown,
			Summary:         "Гра створила файл звіту про крах. Повний опис помилки доступний у файлі.",
			RawExcerpt:      compact(extractReportHeader(reportContent)),
			CrashReportFile: crashReportFile,
		}
	}
	return nil
}

func attachReport(d *Diagnosis, reportFile string) *Diagnosis {
	if reportFile != "" {
		d.CrashReportFile = reportFile
	}
	return d
}

// ── Завантаження правил ─────────────────────────────────────────────────

// LoadRules повертає список правил: спершу користувацький файл (якщо є) —
// вищий пріоритет (перше співпадіння виграє), потім вбудований.
func LoadRules(userRulesPath string) []Rule {
	rules := cachedRules
	if rules != nil {
		return rules
	}
	combined := parseRulesJSON(bundledRulesJSON)
	if userRulesPath != "" {
		if data, err := os.ReadFile(userRulesPath); err == nil {
			if userRules := parseRulesJSON(data); len(userRules) > 0 {
				combined = append(userRules, combined...)
			}
		}
	}
	cachedRulesOnce.Do(func() {
		cachedRules = combined
	})
	return combined
}

// ReloadRules скидає кеш правил (після редагування користувацького файлу).
func ReloadRules() {
	cachedRules = nil
	cachedRulesOnce = sync.Once{}
}

func parseRulesJSON(data []byte) []Rule {
	var doc struct {
		Rules []struct {
			ID             string   `json:"id"`
			Loader         string   `json:"loader"`
			Kind           string   `json:"kind"`
			Regex          string   `json:"regex"`
			Dotall         bool     `json:"dotall"`
			RequesterGroup int      `json:"requesterGroup"`
			MissingGroup   int      `json:"missingGroup"`
			ConflictGroups []int    `json:"conflictGroups"`
			Summary        string   `json:"summary"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []Rule
	for _, r := range doc.Rules {
		pattern := r.Regex
		if r.Dotall {
			pattern = "(?s)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue // невалідне правило — пропускаємо
		}
		kind := Kind(strings.ToUpper(strings.TrimSpace(r.Kind)))
		if kind != KindMissingDependency && kind != KindModConflict {
			continue
		}
		out = append(out, Rule{
			ID:             r.ID,
			Loader:         LoaderScope(strings.ToLower(strings.TrimSpace(r.Loader))),
			Kind:           kind,
			Pattern:        re,
			RequesterGroup: r.RequesterGroup,
			MissingGroup:   r.MissingGroup,
			ConflictGroups: r.ConflictGroups,
			Summary:        r.Summary,
		})
	}
	return out
}

// ── Застосування правил ─────────────────────────────────────────────────

func applyRules(rules []Rule, kind Kind, text, modsDir string, loader LoaderScope) *Diagnosis {
	ordered := orderByLoaderPriority(rules, kind, loader)
	for _, rule := range ordered {
		m := rule.Pattern.FindStringSubmatchIndex(text)
		if m == nil {
			continue
		}
		matchStr := text[m[0]:m[1]]
		switch rule.Kind {
		case KindMissingDependency:
			requester := groupOrNull(m, rule.RequesterGroup, text)
			missing := groupOrNull(m, rule.MissingGroup, text)
			if missing == "" {
				continue
			}
			return buildMissingDepDiagnosis(rule, requester, missing, matchStr, modsDir)
		case KindModConflict:
			var names []string
			for _, g := range rule.ConflictGroups {
				if v := groupOrNull(m, g, text); v != "" {
					names = append(names, v)
				}
			}
			if len(names) == 0 {
				continue
			}
			return buildConflictDiagnosis(rule, names, matchStr, modsDir)
		}
	}
	return nil
}

// groupOrNull витягує значення regex-групи з match-index масиву.
func groupOrNull(m []int, groupIndex int, text string) string {
	if groupIndex <= 0 || groupIndex*2+1 >= len(m) || m[groupIndex*2] < 0 {
		return ""
	}
	return text[m[groupIndex*2]:m[groupIndex*2+1]]
}

func orderByLoaderPriority(rules []Rule, kind Kind, loader LoaderScope) []Rule {
	var matching []Rule
	for _, r := range rules {
		if r.Kind == kind {
			matching = append(matching, r)
		}
	}
	if loader == LoaderAny {
		return matching
	}
	var same, rest []Rule
	for _, r := range matching {
		if r.Loader == loader {
			same = append(same, r)
		} else {
			rest = append(rest, r)
		}
	}
	return append(same, rest...)
}

func buildMissingDepDiagnosis(rule Rule, requester, missing, matchStr, modsDir string) *Diagnosis {
	culprit := ""
	if requester != "" {
		culprit = findJarForModName(requester, modsDir)
	}
	return &Diagnosis{
		Kind:               KindMissingDependency,
		Summary:            renderTemplate(rule.Summary, requester, missing, rule.RequesterGroup),
		RawExcerpt:         compact(matchStr),
		MissingDepName:     missing,
		CulpritModFilename: culprit,
	}
}

func buildConflictDiagnosis(rule Rule, names []string, matchStr, modsDir string) *Diagnosis {
	var candidates []string
	for _, name := range names {
		if jar := findJarForModName(name, modsDir); jar != "" {
			candidates = append(candidates, jar)
		}
	}
	if len(candidates) == 0 {
		candidates = names
	}
	culprit := ""
	if len(candidates) > 0 {
		culprit = candidates[0]
	}
	return &Diagnosis{
		Kind:               KindModConflict,
		Summary:            renderTemplate(rule.Summary, names[0], "", rule.RequesterGroup),
		RawExcerpt:         compact(matchStr),
		ConflictingMods:    candidates,
		CulpritModFilename: culprit,
	}
}

// renderTemplate підставляє у шаблон "{1}" значення першої групи тощо.
func renderTemplate(template, g1, g2 string, requesterGroup int) string {
	if template == "" {
		return "Розпізнано відому проблему."
	}
	values := map[int]string{1: g1, 2: g2}
	out := templatePlaceholder.ReplaceAllStringFunc(template, func(ph string) string {
		idx := strings.Trim(ph, "{}")
		var num int
		for _, ch := range idx {
			if ch < '0' || ch > '9' {
				return ph
			}
			num = num*10 + int(ch-'0')
		}
		if v, ok := values[num]; ok && v != "" {
			return v
		}
		return "?"
	})
	return out
}

// ── Детектори, що не вписуються у формат простих груп ───────────────────

func detectMixinConflict(text string) *Diagnosis {
	loc := mixinApplyError.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	excerpt := text[loc[0]:loc[1]]
	window := excerptWindow(text, loc[0], loc[1]+200)
	jar := firstJarFromStacktrace(window)
	summary := "Конфлікт Mixin-патчів між модами під час завантаження класів"
	if jar != "" {
		summary += " (ймовірно пов'язано з " + jar + ")"
	}
	summary += "."
	candidates := []string{}
	if jar != "" {
		candidates = []string{jar}
	}
	return &Diagnosis{
		Kind:               KindModConflict,
		Summary:            summary,
		RawExcerpt:         compact(excerpt),
		ConflictingMods:    candidates,
		CulpritModFilename: jar,
	}
}

func detectSingleModError(text, modsDir string) *Diagnosis {
	matches := fallbackExceptionHeader.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return nil
	}
	last := matches[len(matches)-1]
	lastExceptionLine := text[last[0]:last[1]]

	window := excerptWindow(text, last[0], min(last[1]+1200, len(text)))
	jar := firstJarFromStacktrace(window)
	if jar == "" {
		return nil
	}
	exceptionType := extractExceptionType(lastExceptionLine)
	return &Diagnosis{
		Kind:               KindSingleModError,
		Summary:            "Помилка \"" + exceptionType + "\" виникла в моді з файлу " + jar + ".",
		RawExcerpt:         compact(window),
		ConflictingMods:    []string{jar},
		CulpritModFilename: jar,
	}
}

// ── Пошкодження файлів оточення (Java / нативки / асети / jar) ─────────

// Патерни, що вказують на ПОШКОДЖЕНІ/ВІДСУТНІ файли оточення — на відміну
// від конфліктів модів. Такі краші виправляються кнопкою «Полагодити»
// (перевірка цілісності + докачка), тому детектор виставляє
// RepairSuggested=true, і AI-панель консолі підкаже користувачу кнопку.
var (
	nativesCorrupt = regexp.MustCompile(`(?is)unsatisfiedlinkerror.{0,600}(?:jna|natives|dll|\bdylib\b)`)
	// NoClassDefFoundError у системних пакетах (org.lwjgl, io.netty, com.mojang,
	// java.*) = бібліотека пошкоджена або недокачана.
	sysClassMissing = regexp.MustCompile(`(?is)noclassdeffounderror[^\n]*?(?:org[./]lwjgl|io[./]netty|com[./]mojang|java[./]lang|javax[./]crypto)`)
	// "Invalid or corrupt jarfile" — сам клієнтський jar/бібліотека пошкоджені.
	corruptJarfile = regexp.MustCompile(`(?i)(?:invalid or corrupt jarfile|corrupt zip|zip\s*error|error opening zip file|could not read zip)`)
	// Пошкоджені/відсутні асети (текстури/звуки) — рідко вбивають гру, але
	// при масовому пошкодженні ресурс-паку краш під час завантаження світу.
	corruptAsset = regexp.MustCompile(`(?is)(?:failed to load (?:texture|sound|resource)|invalid(?:\b|\s)image data|corrupt(?:ed)? (?:image|sound|resource|asset))`)
	// Несумісна/зламана Java-бібліотека нативок або брак файлів JVM.
	jvmNativeBroken = regexp.MustCompile(`(?is)(?:error loading native library|can'?t find native|failed to (?:load|extract) native)`)
	// Бібліотека не знайдена при завантаженні класів (не jar мода) —
	// означає недокачаний/пошкоджений assets/indexes/versions файл.
	libraryNotFound = regexp.MustCompile(`(?i)(?:could not find|unable to locate|no such file)[^\n]*(?:(?:library|asset|libraries|index|version)[^\n]*\.(?:jar|json|pack|png)|\.(?:jar|json|pack|png)[^\n]*(?:library|asset|libraries|index|version))`)
)

// detectEnvCorruption розпізнає пошкодження файлів оточення. Повертає
// діагноз з RepairSuggested=true, щоб AI-панель запропонувала «Полагодити».
func detectEnvCorruption(text string) *Diagnosis {
	pairs := []struct {
		re   *regexp.Regexp
		sum  string
		hint string
	}{
		{nativesCorrupt, "Пошкоджені або відсутні нативні бібліотеки Java (UnsatisfiedLinkError).", "java/natives"},
		{sysClassMissing, "Пошкоджена системна бібліотека Minecraft (недокачаний або зламаний jar).", "libraries"},
		{corruptJarfile, "Файл клієнта або бібліотеки пошкоджений (jar не читається).", "client jar"},
		{corruptAsset, "Пошкоджені або відсутні ресурси гри (текстури/звуки).", "assets"},
		{jvmNativeBroken, "Не вдалося завантажити нативну бібліотеку Java Runtime.", "java/natives"},
		{libraryNotFound, "Не знайдено файл бібліотеки/асетів Minecraft (недокачано).", "libraries/assets"},
	}
	for _, p := range pairs {
		if loc := p.re.FindStringIndex(text); loc != nil {
			return &Diagnosis{
				Kind:            KindEnvCorruption,
				Summary:         p.sum,
				RawExcerpt:      compact(text[loc[0]:loc[1]]),
				RepairSuggested: true,
			}
		}
	}
	return nil
}

// ── Crash-report файл ────────────────────────────────────────────────────

// readCrashReportIfReferenced шукає в логу рядок-вказівник на crash-report і,
// якщо файл існує, повертає його шлях і вміст.
func readCrashReportIfReferenced(logText string) (path, content string) {
	raw := crashReportLocation.FindStringSubmatch(logText)
	if len(raw) > 1 {
		if p := extractReportPath(raw[1]); p != "" {
			if c := readReportFile(p); c != "" {
				return p, c
			}
		}
	}
	// Фолбек: беремо останній збіг (ретраї запуску — цікавить найновіший).
	all := crashReportFallback.FindAllStringSubmatch(logText, -1)
	for _, m := range all {
		if len(m) > 1 {
			if p := extractReportPath(m[1]); p != "" {
				if c := readReportFile(p); c != "" {
					return p, c
				}
			}
		}
	}
	return "", ""
}

func extractReportPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\x00\r\n") {
		return ""
	}
	// Відносні шляхи з crash-reports/ відносно теки гри — лишаємо як є,
	// аналізуючий код вирішить, чи читати (може бути недоступним).
	return raw
}

func readReportFile(p string) string {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() || info.Size() > 5_000_000 {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(data)
}

func extractReportHeader(reportBody string) string {
	if reportBody == "" {
		return ""
	}
	lines := strings.Split(reportBody, "\n")
	var sb strings.Builder
	for i, line := range lines {
		if i >= 20 {
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ── Допоміжні методи ────────────────────────────────────────────────────

func extractExceptionType(line string) string {
	m := exceptionTypePattern.FindString(line)
	if m == "" {
		return "Невідома помилка"
	}
	idx := strings.LastIndex(m, ".")
	if idx >= 0 {
		return m[idx+1:]
	}
	return m
}

func firstJarFromStacktrace(window string) string {
	for _, m := range stacktraceJar.FindAllStringSubmatch(window, -1) {
		jar := m[1]
		if jar == "" {
			jar = m[2]
		}
		if jar != "" && !isLauncherOrJdkJar(jar) {
			return jar
		}
	}
	return ""
}

func isLauncherOrJdkJar(jarName string) bool {
	lower := strings.ToLower(jarName)
	return strings.HasPrefix(lower, "minecraft-") ||
		(strings.Contains(lower, "client-") && strings.Contains(lower, ".jar")) ||
		strings.HasPrefix(lower, "forge-") || strings.HasPrefix(lower, "fabric-loader") ||
		strings.HasPrefix(lower, "quilt-loader") || strings.HasPrefix(lower, "neoforge-") ||
		strings.HasPrefix(lower, "launchwrapper") || strings.HasPrefix(lower, "jna-") ||
		strings.HasPrefix(lower, "guava-") || strings.HasPrefix(lower, "gson-") ||
		strings.HasPrefix(lower, "log4j-") || strings.HasPrefix(lower, "asm-")
}

func excerptWindow(text string, from, to int) string {
	start := max(0, from)
	end := min(len(text), max(to, from+1))
	if start > end {
		return ""
	}
	return text[start:end]
}

// compact стискає багаторядковий уривок до ≤8 значущих рядків.
func compact(excerpt string) string {
	if excerpt == "" {
		return ""
	}
	lines := strings.Split(excerpt, "\n")
	var sb strings.Builder
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(trimmed)
		count++
		if count >= 8 {
			break
		}
	}
	return sb.String()
}

// findJarForModName шукає .jar у теці модів, чия назва містить назву мода.
func findJarForModName(modName, modsDir string) string {
	if modName == "" || modsDir == "" {
		return ""
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return ""
	}
	needle := normalize(modName)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".jar.disabled") {
			if strings.Contains(normalize(name), needle) {
				return name
			}
		}
	}
	return ""
}

func normalize(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// UserRulesPath повертає шлях до користувацького crash-patterns.json у теці
// конфігурації лаунчера (файл створюється при першому запуску, якщо немає).
func UserRulesPath(configDir string) string {
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, "crash-patterns.json")
}
