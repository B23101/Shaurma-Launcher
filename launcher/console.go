package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-wails/internal/ai"
	"shaurma-launcher-wails/internal/builds"
	"shaurma-launcher-wails/internal/console"
	"shaurma-launcher-wails/internal/crashdiag"
	"shaurma-launcher-wails/internal/model"
)

// ── Консоль гри: бекенд-методи (Wails bindings) ──────────────────────────
// Буфер логу живе на бекенді (consoleBuf) і заповнюється завжди, поки гра
// запущена — незалежно від того, відкрите вікно консолі чи ні (див.
// rewireComponents → SetOutputHandler). Ці методи віддають знімок буфера,
// контекст збірки, запускають AI-аналіз крашу та застосовують рекомендовані
// дії.

// GetConsoleSnapshot повертає весь вміст кільцевого буфера консолі (для
// відкриття/повторного відкриття вікна — все, що записалось поки вікно
// було закрите, лишається доступним).
// GetConsoleSnapshot повертає рядки буфера консолі. buildID заданий
// (вкладка «Консоль» на сторінці конкретної збірки) — показуємо буфер
// САМЕ цієї збірки на активному акаунті, НЕ перемикаючи поточний буфер
// (щоб відкриття вкладки не «відбирало» консоль у окремого вікна).
// buildID порожній (окреме вікно консолі) — поточний активний буфер
// (той, за ким вікно слідує: остання запущена збірка).
func (a *App) GetConsoleSnapshot(buildID string) []model.ConsoleLine {
	if buildID != "" {
		// Який акаунт показувати: якщо користувач ПЕРЕМКНУВ консоль на
		// інший акаунт (consoleCurKey збігається з buildID) — рядки його
		// буфера; інакше — активний акаунт (той самий, що в GetConsoleContext).
		// Без цього після перемикання заголовок показував би акаунт Б, а
		// рядки приходили з буфера акаунта А — розбіжність, яку консоль
		// по збірках має виключати.
		accID := ""
		a.consoleBufMu.Lock()
		if a.consoleCurKey.buildID == buildID && a.consoleCurKey.accountID != "" {
			accID = a.consoleCurKey.accountID
		}
		a.consoleBufMu.Unlock()
		if accID == "" {
			accID = a.cfg.ActiveAccount().ID
		}
		if accID != "" {
			a.consoleBufMu.Lock()
			b, ok := a.consoleBufs[consoleKey{accountID: accID, buildID: buildID}]
			if !ok {
				max := 3000
				if s := a.cfg.GetSettings(); s.ConsoleMaxLines > 0 {
					max = s.ConsoleMaxLines
				}
				b = console.NewBuffer(max)
				a.consoleBufs[consoleKey{accountID: accID, buildID: buildID}] = b
			}
			a.consoleBufMu.Unlock()
			return b.Snapshot()
		}
	}
	buf := a.activeConsoleBuf()
	if buf == nil {
		return []model.ConsoleLine{}
	}
	return buf.Snapshot()
}

// ClearConsoleBuffer очищає буфер консолі (кнопка «Очистити»). buildID
// заданий (вкладка збірки) — чистимо буфер саме цієї збірки; порожній
// (окреме вікно) — поточний активний буфер. Разом з рядками чиститься і
// збережений AI-діагноз/краш цієї збірки (інакше після очищення консоль
// показувала б стару AI-панель).
func (a *App) ClearConsoleBuffer(buildID string) {
	if buildID != "" {
		acc := a.cfg.ActiveAccount()
		if acc.ID != "" {
			a.consoleBufMu.Lock()
			if b, ok := a.consoleBufs[consoleKey{accountID: acc.ID, buildID: buildID}]; ok {
				b.Clear()
			}
			a.consoleBufMu.Unlock()
		}
		a.consoleDiagMu.Lock()
		delete(a.consoleExit, buildID)
		delete(a.consoleDiag, buildID)
		a.consoleDiagMu.Unlock()
	} else {
		if buf := a.activeConsoleBuf(); buf != nil {
			buf.Clear()
		}
		a.consoleDiagMu.Lock()
		delete(a.consoleExit, a.consoleCurKey.buildID)
		delete(a.consoleDiag, a.consoleCurKey.buildID)
		a.consoleDiagMu.Unlock()
	}
	// Очищення скоуповане по збірці (console:cleared несе buildID): у режимі
	// «кілька вікон консолі по збірках» лише вікна ЦІЄЇ збірки прибирають
	// AI-панель і рядки; чужі вікна (інші збірки) не зачіпаються.
	a.emit("console:cleared", buildID)
}

// GetConsoleContext повертає відомості про збірку, до якої прив'язана
// консоль: чи запущена гра, назва/версія/лоадер (для заголовка консолі
// та панелі запуску), а також чий акаунт показується зараз і список
// акаунтів з логом цієї збірки (для перемикача консолі). instanceID може
// бути порожнім — тоді береться поточна запущена збірка (остання
// запущена, якщо гра зупинена).
func (a *App) GetConsoleContext(instanceID string) model.ConsoleContext {
	// Порожній instanceID (окреме вікно консолі, відкрите без прив'язки):
	// прив'язуємось до АКТИВНОЇ збірки головного вікна (вибраної/відкритої
	// на сторінці деталей), а не до lastInstanceID — інакше консоль,
	// відкрита без запущеної гри, показувала б застарілу збірку (напр.
	// BlockFront з минулого запуску) з живою кнопкою «Запустити», клік по
	// якій запускав би НЕ ТУ збірку. lastInstanceID — лише фолбек, коли
	// користувач ще не вибирав жодної збірки в головному вікні.
	if instanceID == "" {
		instanceID = a.activeInstanceID
	}
	if instanceID == "" {
		instanceID = a.lastInstanceID
	}
	// Active=true лише коли збірка ВИБРАНА в головному вікні або щойно
	// запускається/запущена. Фолбек на lastInstanceID (збірка з минулого
	// запуску, яку користувач зараз НЕ вибрав) дає Active=false — окреме
	// вікно консолі не показує для неї «Запустити», щоб випадково не
	// запустити не ту збірку.
	active := instanceID == a.activeInstanceID
	// Хто показується в консолі: акаунт, на який перемкнувся користувач
	// (consoleCurKey), інакше — активний акаунт.
	accID := ""
	a.consoleBufMu.Lock()
	if a.consoleCurKey.buildID == instanceID && a.consoleCurKey.accountID != "" {
		accID = a.consoleCurKey.accountID
	}
	a.consoleBufMu.Unlock()
	if accID == "" {
		accID = a.cfg.ActiveAccount().ID
	}
	acc := a.accountByID(accID)

	// «Гра запущена» — для акаунта, ЧИЮ консоль показуємо (не обов'язково
	// активного): незалежні запуски по акаунтах, і перемикач консолі має
	// показувати правильний стан саме того, чий лог на екрані.
	running := false
	if acc.ID != "" && instanceID != "" {
		running = a.sessionsM.IsRunning(acc.ID, instanceID)
	}
	ctx := model.ConsoleContext{
		InstanceID:  instanceID,
		Running:     running,
		AccountID:   acc.ID,
		AccountName: acc.Username,
	}
	if instanceID != "" {
		name, mcVersion, loader := a.packMeta(instanceID)
		if name != "" {
			ctx.Name = name
			ctx.MCVersion = mcVersion
			ctx.Loader = loader
		}
	}
	ctx.RAMMB = a.effectiveRAM(instanceID)
	// Стан краху + останній AI-діагноз — спільні для ВСІХ копій консолі
	// (вікно і вкладка). Консоль, відкрита ПІСЛЯ краху, бачить той самий
	// віджет ШІ, що й консоль, яка була відкрита під час нього: діагноз
	// зберігається на бекенді (consoleDiag), а не у локальному стані
	// конкретної копії.
	if instanceID != "" {
		a.consoleDiagMu.Lock()
		ctx.LastExitCode = a.consoleExit[instanceID]
		ctx.Crashed = a.consoleExit[instanceID] != 0
		if d, ok := a.consoleDiag[instanceID]; ok {
			// Копія діагнозу (щоб фронтенд не мутував спільну мапу).
			cp := d
			ctx.Diagnosis = &cp
		}
		a.consoleDiagMu.Unlock()
	}
	// Реальний стан збірки (той самий, що в sidebar/«Мої збірки») + активний
	// прогрес: панель запуску консолі показує правильну дію (Завантажити /
	// Оновити / Запустити) і живий прогрес, а не фіктивне «Запустити».
	if instanceID != "" {
		status, progress := a.packStatus(instanceID)
		ctx.Status = string(status)
		ctx.Progress = progress

		// Активна стадія запуску (ensure-цикл йде саме зараз): консоль
		// показує реальний стан збірки, а не фіктивне «Запустити» — навіть
		// якщо вікно консолі відкрили посеред запуску.
		a.launchStateMu.Lock()
		lp, ok := a.launchState[instanceID]
		a.launchStateMu.Unlock()
		if ok && !lp.Done {
			ctx.Launching = true
			ctx.LaunchStage = string(lp.Stage)
			ctx.LaunchMessage = lp.Message
			ctx.LaunchPercent = lp.Percent
		}
	}
	ctx.Accounts = a.GetConsoleAccounts(instanceID)
	ctx.Active = active || running || (instanceID != "" && a.isLaunchTracked(instanceID))
	return ctx
}

// packStatus повертає РЕАЛЬНИЙ стан файлів збірки + активний прогрес —
// той самий, яким живляться sidebar і «Мої збірки» (buildView/
// customBuildView): not-installed / needs-update / ready / downloading /
// updating. Активна синхронізація (dlTransient) має найвищий пріоритет.
func (a *App) packStatus(instanceID string) (builds.Status, *builds.Progress) {
	a.dlTransientMu.Lock()
	p, syncing := a.dlTransient[instanceID]
	a.dlTransientMu.Unlock()
	if syncing {
		_, installed := a.buildsReg.Get(instanceID)
		if installed {
			return builds.StatusUpdating, p
		}
		return builds.StatusDownloading, p
	}

	entry, installed := a.buildsReg.Get(instanceID)
	if !installed {
		return builds.StatusNotInstalled, nil
	}

	// Кастомні збірки не мають remote-версії — якщо встановлені, готові.
	if a.customPacks != nil {
		if _, ok := a.customPacks.Get(instanceID); ok {
			return builds.StatusReady, nil
		}
	}
	// Шаурма-збірка: порівнюємо встановлену версію з версією на worker.
	if a.packs != nil {
		if e, err := a.packs.GetPackByID(instanceID); err == nil && e != nil {
			remote := firstNonEmpty(e.Version, e.UpdatedAt)
			if remote != "" && entry.InstalledVersion != remote {
				return builds.StatusNeedsUpdate, nil
			}
		}
	}
	return builds.StatusReady, nil
}

// accountByID повертає акаунт за ID (порожній, якщо не знайдено).
func (a *App) accountByID(id string) model.Account {
	for _, acc := range a.cfg.GetAccounts() {
		if acc.ID == id {
			return acc
		}
	}
	return model.Account{}
}

// GetConsoleAccounts повертає список акаунтів, що мають лог/запуск для
// вказаної збірки — для перемикача консолі (той самий перемикач, що в
// сайдбарі: кожен акаунт бачить незалежний стан і незалежний лог).
func (a *App) GetConsoleAccounts(buildID string) []model.ConsoleAccountInfo {
	var out []model.ConsoleAccountInfo
	for _, acc := range a.cfg.GetAccounts() {
		hasBuf := false
		a.consoleBufMu.Lock()
		_, hasBuf = a.consoleBufs[consoleKey{accountID: acc.ID, buildID: buildID}]
		a.consoleBufMu.Unlock()
		running := buildID != "" && a.sessionsM.IsRunning(acc.ID, buildID)
		if !hasBuf && !running {
			continue
		}
		out = append(out, model.ConsoleAccountInfo{
			AccountID:   acc.ID,
			AccountName: acc.Username,
			Uuid:        acc.UUID,
			Licensed:    acc.IsLicensed,
			Running:     running,
		})
	}
	return out
}

// SwitchConsoleAccount перемикає консоль на лог конкретного акаунта для
// збірки (перемикач у консолі): створює/активує його буфер і змушує вікно
// перечитати знімок + контекст (console:focus).
func (a *App) SwitchConsoleAccount(accountID, buildID string) {
	if accountID == "" {
		accountID = a.cfg.ActiveAccount().ID
	}
	if accountID == "" || buildID == "" {
		return
	}
	a.consoleBufFor(accountID, buildID)
	a.emit("console:focus", buildID)
}

// effectiveRAM повертає поточний ефективний RAM збірки. Пріоритет:
// кастомна збірка з UseCustomRAM → per-instance override (icfg) →
// глобальний MaxRAM.
func (a *App) effectiveRAM(instanceID string) int {
	s := a.cfg.GetSettings()
	ram := s.MaxRAM
	if instanceID != "" {
		if cp := a.customPackOrNil(instanceID); cp != nil && cp.UseCustomRAM && cp.MaxRAMMB > 0 {
			return cp.MaxRAMMB
		}
		icfg := a.cfg.GetInstanceConfig(instanceID)
		if icfg.MaxRAMOverride > 0 {
			ram = icfg.MaxRAMOverride
		}
	}
	return ram
}

// AnalyzeCrash — головний метод «розумної консолі»: спершу ЛОКАЛЬНИЙ
// детектор (crashdiag, без мережі) визначає тип проблеми і витягує факти,
// потім (якщо consoleAI увімкнене) Gemini пише людське пояснення обраною
// мовою та уточнює структуровані поля для UI. Якщо AI недоступний — UI
// показує базовий аналіз від локального детектора (консоль ніколи не
// лишається без інформації через мережеву помилку).
func (a *App) AnalyzeCrash(instanceID string) model.AIDiagnosis {
	s := a.cfg.GetSettings()

	// TTL-дедуп: якщо для цієї збірки вже є СВІЖИЙ діагноз (менше ніж
	// 30 секунд тому) — повертаємо його, не запускаючи повторний (дорогий)
	// виклик Gemini. Це важливо, бо на краш реагують ОДНОЧАСНО дві копії
	// консолі (вікно і вкладка) — інакше ШІ викликався б двічі, і відповіді
	// могли б трохи розходитись.
	a.consoleDiagMu.Lock()
	if at, ok := a.consoleDiagAt[instanceID]; ok && time.Since(at) < 30*time.Second {
		if d, ok2 := a.consoleDiag[instanceID]; ok2 {
			cp := d
			a.consoleDiagMu.Unlock()
			return cp
		}
	}
	a.consoleDiagMu.Unlock()

	// Буфер КОНКРЕТНОЇ збірки (активний акаунт × instanceID), а не просто
	// «поточний»: у режимі кількох вікон консолі по збірках поточний буфер
	// може належати ІНШІЙ збірці, і діагноз рахувався б з чужого логу.
	buf := a.buildConsoleBuf(instanceID)
	lines := []string{}
	if buf != nil {
		for _, l := range buf.Recent(400) {
			lines = append(lines, l.Text)
		}
	}

	loader := crashdiag.LoaderAny
	modsDir := ""
	if instanceID != "" {
		_, _, rawLoader := a.packMeta(instanceID)
		loader = loaderScope(rawLoader)
		modsDir = filepath.Join(s.InstanceDir, instanceID, ".minecraft", "mods")
	}

	diag := crashdiag.Analyze(lines, modsDir, loader, crashdiag.UserRulesPath(a.cfg.ConfigDir()))

	res := model.AIDiagnosis{
		AIEnabled:    s.ConsoleAI,
		CurrentRAMMB: a.effectiveRAM(instanceID),
	}
	if diag == nil {
		res.Kind = string(crashdiag.KindUnknown)
		res.Summary = "Не вдалось розпізнати причину автоматично."
	} else {
		res.Kind = string(diag.Kind)
		res.Summary = diag.Summary
		res.RawExcerpt = diag.RawExcerpt
		res.CrashReportFile = diag.CrashReportFile
		res.Facts = buildFacts(diag)
		if diag.RepairSuggested {
			// Пошкодження файлів оточення — AI-панель консолі підкаже кнопку
			// «Полагодити» і де вона (розділ «Продуктивність» на сторінці
			// збірки) або запропонує дію прямо тут, у консолі.
			res.RepairSuggested = true
			res.RepairHint = "Файли гри/Java пошкоджені або відсутні. Скористайтесь кнопкою «Полагодити»: розділ «Продуктивність» на сторінці збірки або кнопка прямо тут, у консолі. Кнопка перевірить цілісність і перезавантажить лише пошкоджені/відсутні файли — світи, моди та налаштування не постраждають."
		}
		// Рядки-винуватці — теж з буфера КОНКРЕТНОЇ збірки (той самий buf
		// вище), а не «поточного» (може належати іншій збірці).
		if buf != nil {
			res.CulpritLines = findCulpritLines(buf.Recent(400), diag.RawExcerpt)
		}
	}

	// Gemini — лише якщо увімкнене налаштування consoleAI.
	if !s.ConsoleAI {
		a.storeConsoleDiag(instanceID, res)
		return res
	}

	factsJSON, _ := json.Marshal(res.Facts)
	aiRes, err := a.aiClient().AnalyzeCrash(context.Background(), s.Language, res.Kind, string(factsJSON), a.logWindowForAI(instanceID, res))
	if err != nil {
		res.AIAvailable = false
		res.AIError = err.Error()
		return res
	}
	res.AIAvailable = true
	res.AIResult = &aiRes
	a.storeConsoleDiag(instanceID, res)
	return res
}

// storeConsoleDiag зберігає діагноз на бекенді (з часом для TTL-дедупу) і
// шле подію console:ai з buildID, щоб усі відкриті копії консолі САМЕ ЦІЄЇ
// збірки (її окреме вікно + вкладка на сторінці) оновилися ОДНАКОВОЮ
// відповіддю ШІ — без розбіжностей між ними і БЕЗ «просочування» діагнозу
// в консолі інших збірок (у режимі кількох вікон по збірках).
func (a *App) storeConsoleDiag(instanceID string, d model.AIDiagnosis) {
	a.consoleDiagMu.Lock()
	a.consoleDiag[instanceID] = d
	a.consoleDiagAt[instanceID] = time.Now()
	a.consoleDiagMu.Unlock()
	a.emit("console:ai", model.ConsoleAIEvent{BuildID: instanceID, Diagnosis: d})
}

// logWindowForAI готує текст для Gemini: повний crash-report (якщо файл
// знайдено) або останні рядки консолі з перевагою WARN/ERROR (не весь
// INFO-шум), за константою CRASH_ANALYSIS_WINDOW=400 з JavaFX-версії.
// Рядки беруться з буфера КОНКРЕТНОЇ збірки (buildConsoleBuf) — та сама
// гарантія, що в AnalyzeCrash: аналізуємо лог саме тієї збірки, що впала.
func (a *App) logWindowForAI(instanceID string, res model.AIDiagnosis) string {
	if res.CrashReportFile != "" {
		if data, err := os.ReadFile(res.CrashReportFile); err == nil && len(data) <= 1_000_000 {
			return string(data)
		}
	}
	buf := a.buildConsoleBuf(instanceID)
	if buf == nil {
		return ""
	}
	var sb strings.Builder
	for _, l := range buf.Recent(400) {
		if l.Level == console.LevelInfo {
			continue // INFO-шум Gemini не потрібен
		}
		sb.WriteString(l.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildConsoleBuf повертає буфер консолі пари (активний акаунт × buildID)
// БЕЗ зміни «поточного» буфера (на відміну від consoleBufFor, який робить
// буфер поточним для відображення). Потрібен AI-аналізу/лог-вікну: вони
// працюють з логом КОНКРЕТНОЇ збірки, не відбираючи консоль в інших вікон.
// Якщо буфера пари ще немає — фолбек на поточний буфер (збірка щойно
// запускається, лог ще не створений; поточний принаймні не порожній).
func (a *App) buildConsoleBuf(buildID string) *console.Buffer {
	if buildID == "" {
		return a.activeConsoleBuf()
	}
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		return nil
	}
	a.consoleBufMu.Lock()
	b, ok := a.consoleBufs[consoleKey{accountID: acc.ID, buildID: buildID}]
	a.consoleBufMu.Unlock()
	if ok && b != nil {
		return b
	}
	return a.activeConsoleBuf()
}

// buildFacts збирає людські факти з діагнозу локального детектора.
func buildFacts(d *crashdiag.Diagnosis) []string {
	var facts []string
	if d == nil {
		return facts
	}
	if d.MissingDepName != "" {
		facts = append(facts, "Відсутня залежність: "+d.MissingDepName)
	}
	if d.CulpritModFilename != "" {
		facts = append(facts, "Ймовірно винний файл: "+d.CulpritModFilename)
	}
	for _, m := range d.ConflictingMods {
		facts = append(facts, "Фігурує в конфлікті: "+m)
	}
	if d.CrashReportFile != "" {
		facts = append(facts, "Звіт про крах: "+d.CrashReportFile)
	}
	return facts
}

// findCulpritLines шукає у логу точні рядки, що містять уривок, з якого
// зроблено висновок (для точкової підсвітки в консолі — 1-2 рядки).
func findCulpritLines(lines []model.ConsoleLine, excerpt string) []string {
	if excerpt == "" {
		return nil
	}
	// Беремо непусті значущі рядки уривку (до 3).
	parts := []string{}
	for _, ln := range strings.Split(excerpt, "\n") {
		t := strings.TrimSpace(ln)
		if len(t) >= 6 {
			parts = append(parts, t)
		}
		if len(parts) >= 3 {
			break
		}
	}
	var out []string
	for _, l := range lines {
		for _, p := range parts {
			if strings.Contains(l.Text, p) {
				out = append(out, l.Text)
				break
			}
		}
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func loaderScope(raw string) crashdiag.LoaderScope {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fabric":
		return crashdiag.LoaderFabric
	case "forge":
		return crashdiag.LoaderForge
	case "neoforge":
		return crashdiag.LoaderNeoForge
	case "quilt":
		return crashdiag.LoaderQuilt
	case "vanilla":
		return crashdiag.LoaderVanilla
	}
	return crashdiag.LoaderAny
}

// ApplyAIFix застосовує рекомендовану AI дію до збірки:
//   - increase_ram: оновлює InstanceConfig.MaxRAMOverride і перезапускає гру
//   - disable_mod:  ToggleMod(instanceID, targetMod, false) — той самий
//     метод, що й у Менеджері модів (без власного os.Rename)
//   - download_dependency: Modrinth-пайплайн ResolveDependency
func (a *App) ApplyAIFix(instanceID string, action model.AIRecommendedAction) model.AIFixResult {
	switch action.Type {
	case "increase_ram":
		if action.SuggestedRamMB <= 0 {
			return model.AIFixResult{Success: false, Message: "Немає рекомендованого значення RAM"}
		}
		icfg := a.cfg.GetInstanceConfig(instanceID)
		icfg.MaxRAMOverride = action.SuggestedRamMB
		if err := a.cfg.SaveInstanceConfig(icfg); err != nil {
			return model.AIFixResult{Success: false, Message: err.Error()}
		}
		if err := a.LaunchInstance(instanceID); err != nil {
			return model.AIFixResult{Success: false, Message: "RAM змінено, але не вдалось запустити гру: " + err.Error()}
		}
		return model.AIFixResult{Success: true, Message: fmt.Sprintf("RAM змінено на %d МБ, гру перезапущено.", action.SuggestedRamMB)}

	case "disable_mod":
		if action.TargetMod == "" {
			return model.AIFixResult{Success: false, Message: "Не вказано мод для вимкнення"}
		}
		s := a.cfg.GetSettings()
		modsDir := filepath.Join(s.InstanceDir, instanceID, ".minecraft", "mods")
		if err := a.modMgr.ToggleMod(modsDir, action.TargetMod, false); err != nil {
			return model.AIFixResult{Success: false, Message: "Не вдалось вимкнути мод: " + err.Error()}
		}
		return model.AIFixResult{Success: true, Message: "Мод \"" + action.TargetMod + "\" вимкнено. Запустіть гру знову."}

	case "download_dependency":
		if action.DependencyName == "" {
			return model.AIFixResult{Success: false, Message: "Не вказано залежність"}
		}
		s := a.cfg.GetSettings()
		_, mcVersion, loader := a.packMeta(instanceID)
		if mcVersion == "" {
			return model.AIFixResult{Success: false, Message: "Збірку не знайдено"}
		}
		modsDir := filepath.Join(s.InstanceDir, instanceID, ".minecraft", "mods")
		res := a.modrinth.ResolveDependency(action.DependencyName, mcVersion, loader, modsDir)
		return model.AIFixResult{
			Success:     res.Success,
			Message:     res.Message,
			ProjectURL:  res.ProjectURL,
			Source:      res.Source,
			InstalledTo: res.InstalledTo,
		}

	case "repair":
		// Пошкоджені/відсутні файли оточення (Java/нативки/асети/jar):
		// «Полагодити» перевіряє цілісність і докачує лише зламане.
		// Для кастомних збірок іде через EnsureVersion (СИНХРОННО — результат
		// відомий одразу), для Шаурма — через примусовий пересинк з
		// маніфестом (АСИНХРОННО: запускається рунер, і це не означає, що
		// файли вже перевірено — тому повідомлення різні).
		kind, _ := a.packKind(instanceID)
		if err := a.RepairPack(instanceID); err != nil {
			return model.AIFixResult{Success: false, Message: "Не вдалось полагодити збірку: " + err.Error()}
		}
		if kind == "shaurma" {
			return model.AIFixResult{Success: true, Message: "Полагодження запущено: файли збірки перевіряються й оновлюються. Прогрес видно в панелі консолі."}
		}
		return model.AIFixResult{Success: true, Message: "Полагодження завершено: відсутні/пошкоджені файли гри та Java перевірено й завантажено заново. Спробуйте запустити гру знову."}

	default:
		return model.AIFixResult{Success: false, Message: "Немає дії для цього типу рекомендації"}
	}
}

// UploadConsoleLog вивантажує лог на mclo.gs — той самий безкоштовний
// сервіс без API-ключа, що й у старому лаунчері (ConsoleScreen.java):
// POST https://api.mclo.gs/1/log з form-body content=<encoded>.
func (a *App) UploadConsoleLog(text string) (string, error) {
	body := "content=" + url.QueryEscape(text)
	req, err := http.NewRequest("POST", "https://api.mclo.gs/1/log", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mclo.gs HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Success bool   `json:"success"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if !parsed.Success || parsed.URL == "" {
		return "", fmt.Errorf("mclo.gs: завантаження не вдалось")
	}
	return parsed.URL, nil
}

// SaveConsoleLog зберігає лог у .txt-файл у теці логів лаунчера (кнопка
// «Експортувати» — саме .txt з логами консолі, як просив користувач).
func (a *App) SaveConsoleLog(text string) (string, error) {
	logsDir := a.cfg.LogsDir()
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(logsDir, "game-console-"+time.Now().Format("2006-01-02_15-04-05")+".txt")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// SetConsoleRAM змінює RAM збірки (повзунок у AI-панелі increase_ram)
// без перезапуску (перезапуск робить фронтенд кнопкою «Застосувати й
// запустити знову» через ApplyAIFix).
func (a *App) SetConsoleRAM(instanceID string, ramMB int) error {
	icfg := a.cfg.GetInstanceConfig(instanceID)
	icfg.MaxRAMOverride = ramMB
	return a.cfg.SaveInstanceConfig(icfg)
}

// aiClient — ліниво створюваний клієнт Gemini (ключ з env/файлу/дефолт).
func (a *App) aiClient() *ai.Client {
	if a.ai == nil {
		a.ai = ai.NewClient("", a.cfg.ConfigDir(), nil)
	}
	return a.ai
}
