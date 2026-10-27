package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/ai"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
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
				max := 10000
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
		// Іконка збірки (PNG/пресет) + колір — щоб окреме вікно консолі
		// показувало ту саму іконку, що й sidebar/картка головного вікна
		// (воно не отримує пропси packIconUrl/packIcon/packColor).
		iconURL, icon, color := a.packIconMeta(instanceID)
		ctx.IconURL = iconURL
		ctx.Icon = icon
		ctx.Color = color
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
	// Виняток — авто-збірки (див. autoFileTarget): там версія файлів має
	// збігатись із поточною версією конфіга (NeedsUpdate після виходу
	// нової версії Minecraft).
	if a.customPacks != nil {
		if cp, ok := a.customPacks.Get(instanceID); ok {
			return builds.ComputeFileStatus(installed, entry.InstalledVersion, a.autoFileTarget(cp)), nil
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
//
// effectiveRAM повертає RAM, яку гра РЕАЛЬНО отримає при запуску цієї
// збірки (для UI консолі, попереджень про RAM і панелі "Продуктивність").
//
// ВАЖЛИВО: тут НАВМИСНО немає жодного авто-підбору/евристики під фізичну
// пам'ять — раніше тут була перевірка "якщо ram == 4096 (заводський
// дефолт) — підмінити на autoGameRAM(...)". Вона виглядала безпечною
// ("міняємо лише дефолт, явний вибір користувача поважаємо"), але 4096 —
// це ЗАВОДСЬКИЙ дефолт, а НЕ маркер "користувач нічого не обирав": якщо
// людина сама зайшла в Налаштування і поставила рівно 4 ГБ — код не міг
// відрізнити це від "ніхто не чіпав налаштування" і тихо підміняв її вибір
// на менше значення. Через це:
//  1. Попередження "нижче рекомендованої RAM" звірялося з підміненим
//     числом, а не з тим, що людина реально бачила в налаштуваннях —
//     тому могло як не спрацювати, коли мало б, так і спрацювати на
//     число, якого користувач не встановлював.
//  2. Панель консолі показувала RAM, що НЕ ЗБІГАЛАСЯ з тим, що реально
//     пішло в команду запуску Java (LaunchInstance рахує maxRAM окремо,
//     через ту саму формулу intOverride, але БЕЗ автопідбору) — тобто
//     сама цифра в консолі могла бути неправдивою.
//
// Дзеркалимо РІВНО ту саму формулу, що й реальний запуск (LaunchInstance):
// override збірки, якщо є, інакше глобальне значення — без сюрпризів.
func (a *App) effectiveRAM(instanceID string) int {
	s := a.cfg.GetSettings()
	ram := s.MaxRAM
	if instanceID != "" {
		icfg := a.cfg.GetInstanceConfig(instanceID)
		if cp := a.customPackOrNil(instanceID); cp != nil && cp.UseCustomRAM && cp.MaxRAMMB > 0 {
			// Per-instance override (користувач змінив RAM у слайдері)
			// завжди сильніший за заводське значення збірки.
			return intOverride(cp.MaxRAMMB, icfg.MaxRAMOverride)
		}
		ram = intOverride(s.MaxRAM, icfg.MaxRAMOverride)
	}
	return ram
}

// AnalyzeCrash — головний метод «розумної консолі». ВЕСЬ аналіз крашу
// виконує виключно Gemini: локальний/машинний детектор (crashdiag)
// видалено повністю — він не вмів «думати» і видавав хибні діагнози.
// Лаунчер лише ЗБИРАЄ дані для ШІ: crash-report файл (якщо гра його
// створила) або останні значущі рядки консолі (лише WARN/ERROR, без
// дублікатів) — див. logWindowForAI.
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
	var recent []model.ConsoleLine
	if buf != nil {
		recent = buf.Recent(400)
	}

	// Шукаємо crash-report файл за маркером у логу — це лише ЗБИРАННЯ
	// даних (щоб віддати файл ШІ), а не аналіз причини.
	res := model.AIDiagnosis{
		AIEnabled:       s.ConsoleAI,
		CurrentRAMMB:    a.effectiveRAM(instanceID),
		Kind:            model.AIKindUnknown,
		CrashReportFile: findCrashReportFile(recent),
	}

	// RawLogText (для вкладки «Файл краху» на фронтенді) обчислюється
	// ЗАВЖДИ, незалежно від того, чи увімкнена ШІ (s.ConsoleAI) — це
	// просто читання файлу/вирізка логу, без звернення в мережу, і
	// корисне саме по собі (перегляд/копіювання) навіть коли аналіз ШІ
	// вимкнено в налаштуваннях.
	logWindow := a.logWindowForAI(instanceID, res.CrashReportFile)
	res.RawLogText = logWindow

	// ШІ вимкнено в налаштуваннях (consoleAI=false) — панель консолі не
	// рендериться, аналіз нічого не робить і в мережу не ходить.
	if !s.ConsoleAI {
		a.storeConsoleDiag(instanceID, res)
		return res
	}

	// Контекст збірки для ШІ: версія гри, лоадер (+версія) і список модів —
	// реальні факти, на які модель спирається у діагнозі (конфлікти модів,
	// залежності, версійна сумісність). Типи кнопок-дій консолі обмежують
	// рекомендовану дію тим, що лаунчер реально вміє виконати (ApplyAIFix).
	//
	// HasCrashReport/HasErrorLines — ФАКТИ для ШІ, чи був краш взагалі:
	// вихід із ненульовим кодом буває і без аварії (напр. закриття вікна),
	// і тоді ШІ має повернути UNKNOWN, а не вигадувати винного мода з
	// WARN-попереджень. Без цих прапорців модель судила лише за текстом
	// рядків і могла прийняти попередження мода за причину крашу.
	_, mcVersion, loader, loaderVersion := a.packDeepMeta(instanceID)
	ctx := ai.CrashContext{
		LogWindow:        logWindow,
		MCVersion:        mcVersion,
		Loader:           loader,
		LoaderVersion:    loaderVersion,
		Mods:             a.instanceMods(instanceID),
		AvailableActions: []string{"increase_ram", "disable_mod", "download_dependency", "repair", "none"},
		HasCrashReport:   res.CrashReportFile != "",
		HasErrorLines:    consoleHasErrors(recent),
	}

	// БЕЗКОШТОВНО ПЕРШИМ: Groq (openai/gpt-oss-120b з фолбеком на
	// qwen/qwen3.6-27b, безкоштовний тариф — internal/ai.DefaultGroqModels)
	// — ключ у користувача вже є (той самий, що для перекладу/відгуків
	// браузера). Gemini лишається фолбеком, якщо Groq не відповів (немає
	// ключа, мережева помилка, перевищено ліміт, або обидві Groq-моделі
	// зняті з обслуговування).
	aiRes, aiModel, err := a.analyzeCrashFirst(context.Background(), s.Language, ctx)
	if err != nil {
		res.AIAvailable = false
		res.AIError = err.Error()
		a.storeConsoleDiag(instanceID, res)
		return res
	}
	res.AIAvailable = true
	aiRes.Model = aiModel
	res.AIResult = &aiRes
	res.Kind = sanitizeAIKind(aiRes.Kind)
	a.storeConsoleDiag(instanceID, res)
	return res
}

// analyzeCrashFirst пробує безкоштовний Groq, потім Gemini (flash/lite,
// БЕЗ платної pro-моделі). Повертає (результат, назва моделі, помилка).
//
// ОДИН спільний дедлайн на ВЕСЬ ланцюжок (Groq + Gemini): без нього кожен
// провайдер брав би свої 25с, і кнопка «Проаналізувати» висіла б до 50с.
func (a *App) analyzeCrashFirst(ctx context.Context, lang string, cc ai.CrashContext) (model.AIResult, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	// 1) Groq — безкоштовно. Ліниво створюється з ключа користувача
	//    (Налаштування → Консоль) або groq.key/env/вбудованого.
	if a.groqAPIKeyResolved() != "" {
		gc := a.groqClient()
		if res, err := gc.AnalyzeCrash(ctx, lang, cc); err == nil && res.Kind != "" {
			return res, "groq (" + gc.ModelName() + ")", nil
		}
	}
	// 2) Gemini — фолбек. Ланцюжок БЕЗ gemini-2.5-pro (платна модель):
	//    flash → flash-lite. Якщо й вони недоступні — помилка.
	res, err := a.aiClient().AnalyzeCrash(ctx, lang, cc)
	if err != nil {
		return model.AIResult{}, "", err
	}
	return res, res.Model, nil
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

// logWindowForAI готує дані для Gemini за правилом «є краш → crash report,
// немає → останні 50 рядків консолі»:
//   - Crash-report файл (якщо знайдено): передаємо його ПОВНІСТЮ — там
//     справжній стек-трейс, який ШІ і має аналізувати.
//   - Інакше: лише WARN/ERROR рядки (INFO-шум ШІ не потрібен), зі
//     схлопуванням повторів поспіль (та сама помилка, що пишеться 1000
//     разів, потрапляє в промт ОДИН раз), до 50 рядків — щоб скоротити
//     промт і дати ШІ саме те, що реально потрібно для аналізу.
//
// Рядки беруться з буфера КОНКРЕТНОЇ збірки (buildConsoleBuf) — та сама
// гарантія, що в AnalyzeCrash: аналізуємо лог саме тієї збірки, що впала.
func (a *App) logWindowForAI(instanceID, crashReportFile string) string {
	if crashReportFile != "" {
		if data, err := os.ReadFile(crashReportFile); err == nil && len(data) <= 1_000_000 {
			return string(data)
		}
	}
	buf := a.buildConsoleBuf(instanceID)
	if buf == nil {
		return ""
	}
	return lastNWarnError(buf.Recent(400), 50)
}

// instanceMods повертає список файлів модів (.jar) з теки mods/ збірки —
// до 100 імен, для контексту AI-аналізу (модель бачить реальний склад
// модів, тож діагноз про конфлікти/залежності спирається на факти).
func (a *App) instanceMods(instanceID string) []string {
	if instanceID == "" {
		return nil
	}
	modsDir := filepath.Join(a.gameDir(instanceID), "mods")
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil
	}
	var mods []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".jar") {
			mods = append(mods, e.Name())
			if len(mods) >= 100 {
				break
			}
		}
	}
	return mods
}

// consoleHasErrors повертає true, якщо серед рядків логу є хоча б один
// рівня ERROR (exception/fatal/crash тощо). Це факт для AI-промту: якщо
// ERROR-рядків немає — у логу лише попередження, і ШІ не має права
// стверджувати, що гра впала через мод.
func consoleHasErrors(lines []model.ConsoleLine) bool {
	for _, l := range lines {
		if l.Level == console.LevelError {
			return true
		}
	}
	return false
}

// lastNWarnError повертає останні до n УНІКАЛЬНИХ рядків рівня WARN/ERROR:
// INFO/system відкидаються, а дублікати (та сама помилка, що повторюється
// 1000 разів) пишуться ОДИН раз — щоб скоротити промт для ШІ. Порівняння
// йде за нормалізованим текстом БЕЗ часової мітки (Minecraft додає
// [HH:MM:SS] у кожен рядок, тож інакше однакові помилки ніколи не
// збіглися б). З дублікатів лишається НАЙНОВІШИЙ. Повертає рядки у
// хронологічному порядку (найновіший — останній).
func lastNWarnError(lines []model.ConsoleLine, n int) string {
	var out []string
	seen := make(map[string]struct{})
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		l := lines[i]
		if l.Level != console.LevelWarn && l.Level != console.LevelError {
			continue
		}
		text := strings.TrimSpace(l.Text)
		if text == "" {
			continue
		}
		key := consoleLineDedupKey(text)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, text)
	}
	slices.Reverse(out)
	return strings.Join(out, "\n")
}

// consoleLineDedupKey нормалізує рядок логу для порівняння при дедупі:
// зрізає провідну часову мітку виду "[12:34:56] " (її наявність/відсутність
// не робить рядки різними), а все інше лишає як є.
var consoleTS = regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\]\s*`)

func consoleLineDedupKey(text string) string {
	return consoleTS.ReplaceAllString(text, "")
}

// Маркери, за якими Minecraft пише у stdout/stderr шлях до crash-report
// файлу. Пошук файлу — це ЗБИРАННЯ даних для ШІ, а не аналіз причини.
var (
	crashReportMarker         = regexp.MustCompile(`#@!@# Game crashed! Crash report saved to: #@!@#\s*(?P<path>.+)`)
	crashReportMarkerFallback = regexp.MustCompile(`(?i)crash report saved to:?\s*(?P<path>[^\r\n]+\.txt)`)
)

// findCrashReportFile шукає у рядках логу шлях до crash-report файлу.
func findCrashReportFile(lines []model.ConsoleLine) string {
	for _, l := range lines {
		if m := crashReportMarker.FindStringSubmatch(l.Text); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	for _, l := range lines {
		if m := crashReportMarkerFallback.FindStringSubmatch(l.Text); m != nil {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// sanitizeAIKind нормалізує тип проблеми, який повернув Gemini, до
// відомого набору; невідомі значення → UNKNOWN.
func sanitizeAIKind(k string) string {
	switch strings.ToUpper(strings.TrimSpace(k)) {
	case model.AIKindMissingDependency, model.AIKindModConflict,
		model.AIKindSingleModError, model.AIKindEnvCorruption,
		model.AIKindJavaVersion, model.AIKindUnknown:
		return strings.ToUpper(strings.TrimSpace(k))
	}
	return model.AIKindUnknown
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
		modsDir := filepath.Join(a.gameDir(instanceID), "mods")
		if err := a.modMgr.ToggleMod(modsDir, action.TargetMod, false); err != nil {
			return model.AIFixResult{Success: false, Message: "Не вдалось вимкнути мод: " + err.Error()}
		}
		return model.AIFixResult{Success: true, Message: "Мод \"" + action.TargetMod + "\" вимкнено. Запустіть гру знову."}

	case "download_dependency":
		if action.DependencyName == "" {
			return model.AIFixResult{Success: false, Message: "Не вказано залежність"}
		}
		_, mcVersion, loader := a.packMeta(instanceID)
		if mcVersion == "" {
			return model.AIFixResult{Success: false, Message: "Збірку не знайдено"}
		}
		modsDir := filepath.Join(a.gameDir(instanceID), "mods")
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

// SaveConsoleLog зберігає лог у .txt-файл — З ДІАЛОГОМ вибору місця
// збереження (кнопка «Експортувати»): користувач сам вказує теку і назву
// файлу, а не отримує файл у службовій теці логів. Скасування діалогу
// повертає "" (фронтенд мовчки нічого не робить).
func (a *App) SaveConsoleLog(text string) (string, error) {
	defaultName := "game-console-" + time.Now().Format("2006-01-02_15-04-05") + ".txt"
	path := bridgeDialogSaveFile("Зберегти лог консолі", defaultName, map[string][]string{
		"Текстовий файл (*.txt)": {"txt"},
	})
	if path == "" {
		return "", nil // діалог скасовано — без помилки
	}
	if !strings.HasSuffix(strings.ToLower(path), ".txt") {
		path += ".txt"
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// RevealPath відкриває Провідник із виділеним конкретним файлом/текою
// (кнопка «Відкрити теку» у тості після збереження логу).
func (a *App) RevealPath(path string) error {
	if path == "" {
		return nil
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer.exe", "/select,"+path).Start()
	}
	return xdgOpen(filepath.Dir(path))
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
	// Власний ключ користувача (Налаштування → Консоль) має пріоритет над
	// файлом gemini.key / env / вбудованим дефолтом. Кешуємо лише до зміни
	// ключа — змінили в налаштуваннях, і наступний аналіз уже новий ключ.
	s := a.cfg.GetSettings()
	if a.ai == nil || a.aiKey != s.ConsoleAIKey {
		a.ai = ai.NewClient(s.ConsoleAIKey, a.cfg.ConfigDir(), nil)
		a.aiKey = s.ConsoleAIKey
	}
	return a.ai
}
