package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/java"
	"shaurma-launcher-backend/internal/minecraft"
)

// ── Версії Minecraft / лоадерів (форма "Нова збірка → Вручну") ──────────

// GetCustomPackMCVersions повертає список версій Minecraft для селектора
// версії гри — найновіші перші, як віддає Mojang.
func (a *App) GetCustomPackMCVersions() ([]custompack.MCVersionEntry, error) {
	return a.versionsProv.GetMCVersions()
}

// GetCustomPackLoaderVersions повертає версії конкретного лоадера,
// сумісні з обраною версією Minecraft, відсортовані за FlexVer (найновіші
// перші, як Prism) — перша рекомендована позначається Recommended=true.
// loader: "vanilla" | "fabric" | "forge" | "neoforge" | "quilt".
func (a *App) GetCustomPackLoaderVersions(loader, mcVersion string) ([]custompack.LoaderVersionEntry, error) {
	return a.versionsProv.GetLoaderVersions(custompack.Loader(loader), mcVersion)
}

// GetCustomPackLoaderAvailability повертає лоадери, які мають версії під
// обрану версію Minecraft ("vanilla" завжди перший). Недоступні лоадери
// фронтенд ховає з сітки вибору і перемикається на вільний — так само, як
// Prism показує порожній список "No versions are currently available for
// Minecraft X" для лоадера без версій під цю MC-версію (CustomPage).
func (a *App) GetCustomPackLoaderAvailability(mcVersion string) []string {
	loaders := a.versionsProv.GetAvailableLoaders(mcVersion)
	out := make([]string, 0, len(loaders))
	for _, l := range loaders {
		out = append(out, string(l))
	}
	return out
}

// GetCustomPackDefaultMCVersion — поточна стабільна версія Minecraft, для
// дефолтного значення селектора при відкритті сторінки створення збірки.
func (a *App) GetCustomPackDefaultMCVersion() (string, error) {
	return a.versionsProv.LatestReleaseMCVersion()
}

// HasCurseForgeAPIKey — чи вшито CurseForge API-ключ у цю збірку
// лаунчера. UI показує попередження на вкладці "Імпортувати", якщо ключа
// немає (моди CurseForge не довантажаться автоматично).
func (a *App) HasCurseForgeAPIKey() bool {
	return custompack.NewCurseForgeClient().HasAPIKey()
}

// ── Файлові діалоги для форми створення/імпорту ──────────────────────────

// BrowseImageFile відкриває діалог вибору іконки/фону збірки
// (PNG/JPG/WEBP/GIF — GIF підтримується для анімованих іконок/банерів)
// через generic bridge-діалог (виконує shell.exe).
func (a *App) BrowseImageFile() (string, error) {
	return bridgeDialogOpenFile("Виберіть зображення", false, map[string][]string{
		"Зображення (*.png;*.jpg;*.jpeg;*.webp;*.gif)": {"png", "jpg", "jpeg", "webp", "gif"},
	}), nil
}

// BrowseModpackArchive відкриває діалог вибору файлу модпаку для імпорту
// (.mrpack / .zip / .shaurmpack / .shaurmbundle) — вкладка "Імпортувати".
// Усі формати в ОДНОМУ діалозі (запит: «не має бути окремих кнопок для
// Shaurma-збірок — імпорт іде спільно, як mrpack/zip»): формат файлу
// визначається фронтендом за розширенням (detectArchive у CreatePackPage),
// а не тим, яку кнопку/діалог натиснув користувач.
func (a *App) BrowseModpackArchive() (string, error) {
	return bridgeDialogOpenFile("Виберіть файл модпаку", false, map[string][]string{
		"Модпаки (*.mrpack;*.zip;*.shaurmpack;*.shaurmbundle)": {"mrpack", "zip", "shaurmpack", "shaurmbundle"},
	}), nil
}

// ── Створення / редагування / видалення кастомної збірки (вручну) ───────

// CustomPackFormParams — плаский набір полів форми, зручний для виклику з
// фронтенду (уникає вкладених типів у Wails-бінденні). Дзеркалить
// custompack.CreateParams.
type CustomPackFormParams struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	IconSrcPath       string `json:"iconSrcPath"`
	BackgroundSrcPath string `json:"backgroundSrcPath"`
	// BackgroundRemove — видалити встановлений фон збірки (кнопка
	// «Видалити» біля банера в персоналізації). Див. CreateParams.
	BackgroundRemove bool   `json:"backgroundRemove"`
	Icon             string `json:"icon"` // пресет-іконка (Tabler, напр. "ti-puzzle")
	// IconChanged — true лише коли користувач у ЦЬОМУ збереженні явно
	// торкнувся вибору іконки (клік по пресету або новий файл через
	// діалог). Форма шле поточний iconName у полі Icon завжди, незалежно
	// від того, що редагувалось, тому без цього прапорця Update не міг
	// відрізнити "іконку не чіпали" від "свідомо обрали пресет" — і
	// затирав робочий PNG при кожному збереженні назви/опису/версій.
	IconChanged     bool   `json:"iconChanged"`
	Color           string `json:"color"`
	MCVersion       string `json:"mcVersion"`
	Loader          string `json:"loader"`
	LoaderVersion   string `json:"loaderVersion"`
	UseCustomRAM    bool   `json:"useCustomRam"`
	MinRAMMB        int    `json:"minRamMb"`
	MaxRAMMB        int    `json:"maxRamMb"`
	UseSeparateJava bool   `json:"useSeparateJava"`
	JavaPath        string `json:"javaPath"`
}

func (p CustomPackFormParams) toCreateParams() custompack.CreateParams {
	return custompack.CreateParams{
		Name:              p.Name,
		Description:       p.Description,
		IconSrcPath:       p.IconSrcPath,
		BackgroundSrcPath: p.BackgroundSrcPath,
		BackgroundRemove:  p.BackgroundRemove,
		Icon:              p.Icon,
		IconChanged:       p.IconChanged,
		Color:             p.Color,
		MCVersion:         p.MCVersion,
		Loader:            custompack.Loader(p.Loader),
		LoaderVersion:     p.LoaderVersion,
		UseCustomRAM:      p.UseCustomRAM,
		MinRAMMB:          p.MinRAMMB,
		MaxRAMMB:          p.MaxRAMMB,
		UseSeparateJava:   p.UseSeparateJava,
		JavaPath:          p.JavaPath,
	}
}

// CreateCustomPack створює нову кастомну збірку (вкладка "Вручну").
// Файли гри не якаються тут — це лише збереження конфігурації; збірка
// одразу позначається ВСТАНОВЛЕНОЮ (готовою до запуску), бо при ручному
// створенні "все вже налаштовано" — вікно прогресу показується лише коли
// це імпорт модпаку (див. ImportModpackArchive) або коли при запуску
// качаються компоненти Minecraft. Статус файлів спільний: "готово",
// без стану "не встановлено".
func (a *App) CreateCustomPack(p CustomPackFormParams) (custompack.Pack, error) {
	// Перевірка ПЕРЕД створенням: чи реально існує версія лоадера для цієї
	// версії Minecraft (захист від збірки, яка при першому запуску падала б
	// з «не можу знайти версію лоадера»). Мережева помилка НЕ блокує
	// створення: невідомо ≠ «версії немає», Create/запуск вирішать.
	if err := a.validateLoaderBeforeCreate(p); err != nil {
		return custompack.Pack{}, err
	}
	pack, err := custompack.Create(a.customPacks, a.cfg.InstancesDir(), p.toCreateParams())
	if err != nil {
		return custompack.Pack{}, err
	}
	// РAM з форми збірки → per-instance конфіг: раніше значення жили лише в
	// записі збірки (їх використовував запуск через UseCustomRAM), а сторінка
	// збірки читала InstanceConfig — тому галочка «використовувати свої
	// налаштування RAM» на сторінці показувалась УВІМКНЕНОЮ за замовчуванням
	// («глобальні»), а введені значення виглядали скинутими. Тепер сіємо
	// конфіг одразу при створенні.
	if pack.UseCustomRAM && pack.MaxRAMMB > 0 {
		cfg := a.cfg.GetInstanceConfig(pack.ID)
		cfg.MaxRAMOverride = pack.MaxRAMMB
		cfg.MinRAMOverride = pack.MinRAMMB
		if err := a.cfg.SaveInstanceConfig(cfg); err != nil {
			// Помилка конфіга НЕ критична сама по собі (запуск все одно читає
			// UseCustomRAM з запису збірки) — але продовжуємо далі: реєстрація
			// «встановлена» нижче КРИТИЧНА, без неї картка показувала б
			// «Завантажити» для щойно створеної збірки.
			_ = err
		}
	}
	// Ручне створення = збірка "просто створена і завантажена": без цього
	// картка показувала б "Завантажити"/вікно прогресу, як для Shaurma-пака,
	// що збивало б з пантелику (див. customBuildView: !installed → not-installed).
	a.buildsReg.SetInstalled(pack.ID, builds.KindCustom, "")
	a.emit("builds:changed", pack.ID)
	// Налаштування «після створення одразу завантажити все необхідне» (як
	// Prism: «download all necessary files now?»): у фоні качаємо Java,
	// лоадер і клієнт Minecraft — перший запуск стає миттєвим. Прогрес іде
	// подіями installer:progress {packId, message, percent} — картка збірки
	// показує «Завантаження…» з прогрес-баром.
	if a.cfg.GetSettings().AutoDownloadAfterCreate {
		a.prepareCustomPackAfterCreate(pack)
	}
	return pack, nil
}

// prepareCustomPackAfterCreate — фонове передзавантаження ВСІХ необхідних
// компонентів щойно створеної кастомної збірки (Java потрібного мажора,
// профіль лоадера, клієнт Minecraft + бібліотеки + асети). Той самий
// набір, що ставиться при першому запуску — але одразу після створення,
// без блокування UI (як Prism). Прогрес: installer:progress {packId, msg,
// percent} (0-100 на всю операцію: Java 0-20, лоадер 20-60, клієнт 60-100),
// завершення — installer:done {packId}, помилка — download:error
// {packId, message}. Скасування (кнопка «Зупинити») тихо завершує процес.
func (a *App) prepareCustomPackAfterCreate(p custompack.Pack) {
	a.prepareCustomPackComponents(p, nil)
}

// prepareCustomPackComponents — спільне тіло фонового встановлення
// компонентів кастомної збірки: Java потрібного мажора, лоадер і клієнт
// Minecraft. Використовується і після створення збірки
// (AutoDownloadAfterCreate), і для «Завантажити» на авто-збірках
// (RefreshAutoPackFiles — onSuccess там фіксує «встановлено під нову
// версію», щоб статус став «Готова»).
func (a *App) prepareCustomPackComponents(p custompack.Pack, onSuccess func()) {
	go func() {
		defer crashguard.Recover("custompack_api.go:209")
		// Горутина не повинна вбити лаунчер падінням.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("prepareCustomPack: panic: %v", r)
				a.emit("installer:done", p.ID)
			}
		}()
		id := p.ID
		progress := func(msg string, pct int) {
			a.emit("installer:progress", map[string]any{"packId": id, "message": msg, "percent": pct})
		}
		fail := func(err error) {
			// Скасування користувачем — не помилка: тихо завершуємось,
			// картка вже повернулась до звичайного стану.
			if isLaunchCancelled(err) {
				a.emit("installer:done", id)
				return
			}
			a.emit("download:error", map[string]any{"packId": id, "message": err.Error()})
			a.emit("installer:done", id)
		}

		progress("Підготовка компонентів...", 0)

		// 1) Java потрібного мажора (та сама тека, що буде при запуску:
		// вбудована або обрана користувачем для збірки).
		recommended := java.RecommendedMajor(p.MCVersion)
		javaDir := a.cfg.JavaDir()
		if p.UseSeparateJava && p.JavaPath != "" {
			javaDir = p.JavaPath
		}
		javInst := a.javaInst
		if javaDir != "" && javaDir != a.cfg.JavaDir() {
			javInst = java.NewInstaller(javaDir)
			javInst.ShareCancelFlagWith(a.javaInst)
		}
		prevJava := javInst.ProgressHandler()
		javInst.SetProgressHandler(func(jp java.Progress) {
			progress(fmt.Sprintf("Завантаження Java %d... %d%%", recommended, jp.Percent), jp.Percent/5)
		})
		javaExe, err := javInst.EnsureMajorWithComponent(recommended, "")
		javInst.SetProgressHandler(prevJava)
		if err != nil {
			fail(fmt.Errorf("Java %d: %w", recommended, err))
			return
		}
		// Forge/NeoForge запускають свої processors під час встановлення —
		// їм потрібна Java (той самий SetProcessorJava, що і при запуску).
		a.inst.SetProcessorJava(javaExe)

		// 2) Лоадер (Fabric/Quilt/Forge/NeoForge профіль; vanilla — сама версія).
		// 3) Клієнт Minecraft + бібліотеки + асети. Прогрес реальних відсотків
		// з EnsureLoader/EnsureVersion (0-100) мапимо на діапазони 20-60/60-100.
		prevInst := a.inst.InstallProgressHandler()
		a.inst.SetInstallProgressHandler(func(ip minecraft.InstallProgress) {
			progress(ip.Message, 20+ip.Percent*40/100)
		})
		launchVersion, err := a.inst.EnsureLoader(p.Loader, p.MCVersion, p.LoaderVersion)
		if err != nil {
			a.inst.SetInstallProgressHandler(prevInst)
			fail(err)
			return
		}
		if _, err := a.inst.EnsureVersion(launchVersion); err != nil {
			a.inst.SetInstallProgressHandler(prevInst)
			fail(err)
			return
		}
		a.inst.SetInstallProgressHandler(prevInst)

		progress("Компоненти готові", 100)
		if onSuccess != nil {
			onSuccess()
		}
		a.emit("installer:done", id)
	}()
}

// validateLoaderBeforeCreate перевіряє, що обрана версія лоадера реально
// існує для обраної версії Minecraft. Джерело — той самий VersionsProvider,
// яким наповнюється форма, тож перевірка дешева (кеш). Мережева помилка
// повертає nil: не можемо підтвердити — не блокуємо створення.
func (a *App) validateLoaderBeforeCreate(p CustomPackFormParams) error {
	loader := custompack.Loader(strings.TrimSpace(p.Loader))
	if loader == "" || loader == custompack.LoaderVanilla {
		return nil
	}
	ver := strings.TrimSpace(p.LoaderVersion)
	if ver == "" {
		return nil // сам Create вимагає версію для не-Vanilla лоадера
	}
	list, err := a.versionsProv.GetLoaderVersions(loader, p.MCVersion)
	if err != nil {
		return nil // мережа недоступна — не блокуємо створення
	}
	for _, v := range list {
		if v.Version == ver {
			return nil
		}
	}
	return fmt.Errorf("версія лоадера %s %s не існує для Minecraft %s — обери версію зі списку у формі",
		loader, ver, p.MCVersion)
}

// UpdateCustomPack редагує вже створену кастому збірку.
// Зміна назви, кольору, опису, іконки, фону — дозволена під запущеною грою.
// Зміна MCVersion/Loader/LoaderVersion — заблокована (ламає запущену сесію).
//
// Авто-збірки (vanilla release/snapshot, isAutoPackID) — ОКРЕМИЙ випадок:
// їхню версію гри й лоадер керує ЛИШЕ фонова синхронізація
// (checkAutoVersionPacks/ensureAutoVersionSync) на основі autoPackRecord.Target,
// а не користувач. Раніше форма дозволяла ввести довільні MCVersion/Loader
// прямо тут — це не ламало запуск одразу, але розсинхронізовувало
// збережений cp.MCVersion із Target: наступна перевірка або мовчки
// перезаписувала ручну правку назад (бо порівнює Target з поточним
// MCVersion), або, якщо Target випадково збігався зі старим значенням,
// нова версія гри взагалі не підхоплювалась при запуску. Тому для
// авто-збірок версію/лоадер тут ігноруємо повністю (примусово залишаємо
// старі значення) — лишається доступне тільки косметичне редагування.
func (a *App) UpdateCustomPack(id string, p CustomPackFormParams) (custompack.Pack, error) {
	old, _ := a.customPacks.Get(id)
	if _, isAuto := a.isAutoPackID(id); isAuto {
		p.MCVersion = old.MCVersion
		p.Loader = old.Loader
		p.LoaderVersion = old.LoaderVersion
	}
	// Блокуємо ТІЛЬКИ зміну версії/лоадера під запущеною грою
	if err := a.errPackRunning(id); err != nil {
		isDestructive := (p.MCVersion != old.MCVersion && p.MCVersion != "") ||
			(p.Loader != old.Loader && p.Loader != "")
		if isDestructive {
			return custompack.Pack{}, err
		}
		// Косметичні зміни (назва, колір, опис, іконка) — дозволені
	}
	pack, err := custompack.Update(a.customPacks, a.cfg.InstancesDir(), id, p.toCreateParams())
	if err != nil {
		return custompack.Pack{}, err
	}
	// Якщо змінилась версія Minecraft — прибираємо кеш старої версії зі
	// спільної бази інсталера (<dataDir>/minecraft/versions/<old>), щоб
	// після зміни версії не лежали зайві/застарілі файли. База спільна
	// для всіх збірок, але це саме кеш: при потребі версія докачається
	// заново через EnsureVersion. Зміна лоадера не лишає файлів, які
	// треба чистити (інсталяторів лоадерів у лаунчері поки немає).
	if old.MCVersion != "" && old.MCVersion != pack.MCVersion {
		_ = os.RemoveAll(filepath.Join(a.cfg.Dir(), "minecraft", "versions", old.MCVersion))
	}
	// Той самий сіяння RAM у per-instance конфіг, що й при створенні: якщо
	// у формі зняли «свою RAM» — чистимо override-и; якщо поставили —
	// записуємо значення. Інакше сторінка збірки після редагування знову
	// показувала б «глобальні» налаштування зі скинутими значеннями.
	if pack.UseCustomRAM && pack.MaxRAMMB > 0 {
		cfg := a.cfg.GetInstanceConfig(pack.ID)
		cfg.MaxRAMOverride = pack.MaxRAMMB
		cfg.MinRAMOverride = pack.MinRAMMB
		a.cfg.SaveInstanceConfig(cfg)
	} else {
		cfg := a.cfg.GetInstanceConfig(pack.ID)
		cfg.MaxRAMOverride = 0
		cfg.MinRAMOverride = 0
		a.cfg.SaveInstanceConfig(cfg)
	}

	// Ярлик на робочому столі (якщо існує): оновлюємо назву/іконку НА МІСЦІ
	// (перейменування .lnk не рухає іконку на робочому столі — позиція
	// зберігається за файлом). Ярлика немає — нічого не робимо: зміна
	// назви збірки не повинна воскрешати видалений користувачем ярлик.
	_ = a.UpdateDesktopShortcut(pack.ID)

	// Оновлюємо instance.json новими метаданими (InstanceConfig-вигляд:
	// назва/версія/лоадер/колір/іконка/фон). Без цього кроку orphaned
	// rebuild (addOrphanedBuild) і syncOrphanedToCustomPacks читали б з
	// instance.json СТАРУ назву, і перейменована збірка після перезапуску
	// знову показувалась під старою назвою (баг «змінив назву й ID —
	// застосувалось тільки щось одне»). saveInstanceConfig зливає поля з
	// наявним файлом, тож унікальні поля Pack (importState, blockedMods,
	// tempMods, sha1…) не втрачаються.
	icfg := a.cfg.GetInstanceConfig(pack.ID)
	icfg.ID = pack.ID
	icfg.Name = pack.Name
	icfg.MCVersion = pack.MCVersion
	icfg.Loader = pack.Loader
	icfg.LoaderVersion = pack.LoaderVersion
	icfg.Color = pack.Color
	icfg.IconPath = pack.IconPath
	icfg.BackgroundPath = pack.BackgroundPath
	icfg.Source = pack.Source
	icfg.Description = pack.Description
	_ = a.cfg.SaveInstanceConfig(icfg)

	a.emit("builds:changed", pack.ID)
	return pack, nil
}

// GetCustomPack повертає повний опис кастомної збірки (для сторінки
// редагування — форма попередньо заповнюється поточними значеннями).
func (a *App) GetCustomPack(id string) (custompack.Pack, error) {
	p, ok := a.customPacks.Get(id)
	if !ok {
		return custompack.Pack{}, fmt.Errorf("кастомну збірку не знайдено: %s", id)
	}
	// Перевіряємо mods/ папку та оновлюємо BlockedMods: якщо файл мода
	// вже є на диску — позначаємо Downloaded = true.
	if len(p.BlockedMods) > 0 {
		changed := false
		for i := range p.BlockedMods {
			if p.BlockedMods[i].Downloaded {
				continue
			}
			modsDir := filepath.Join(a.gameDir(id), "mods")
			if _, err := os.Stat(filepath.Join(modsDir, p.BlockedMods[i].FileName)); err == nil {
				p.BlockedMods[i].Downloaded = true
				changed = true
			}
		}
		if changed {
			_ = a.customPacks.Save(p)
		}
	}
	return p, nil
}

// ToggleBlockedModDownloaded позначає/знімає позначку «скачано» для мода
// з BlockedMods. Якщо файл мода є в mods/ — позначає Downloaded=true,
// якщо ні — Downloaded=false (користувач може вручну зняти позначку).
func (a *App) ToggleBlockedModDownloaded(packID, fileName string, downloaded bool) error {
	p, ok := a.customPacks.Get(packID)
	if !ok {
		return fmt.Errorf("кастомну збірку не знайдено: %s", packID)
	}
	changed := false
	for i := range p.BlockedMods {
		if p.BlockedMods[i].FileName == fileName {
			p.BlockedMods[i].Downloaded = downloaded
			changed = true
			break
		}
	}
	if changed {
		_ = a.customPacks.Save(p)
	}
	return nil
}

// popBlockedCheckedMods витягує та видаляє список позначених галочками
// модів для даної збірки (забирає з blockedCheckedMods). Викликається
// при створенні збірки після завершення імпорту.
func (a *App) popBlockedCheckedMods(packID string) []string {
	a.customImportMu.Lock()
	defer a.customImportMu.Unlock()
	checked := a.blockedCheckedMods[packID]
	delete(a.blockedCheckedMods, packID)
	return checked
}

// CustomPackIDCheck — результат перевірки ID збірки перед створенням.
// Фронтенд показує попередження/модалку, якщо ID уже зайнятий.
type CustomPackIDCheck struct {
	ID        string `json:"id"`        // slug з ефективної назви (той, що буде у збірки)
	Exists    bool   `json:"exists"`    // чи вже є збірка з таким ID
	Suggested string `json:"suggested"` // перший вільний варіант (id, id-2, id-3…)
}

// CheckCustomPackID перевіряє, чи ID, згенерований з назви, уже зайнятий
// іншою збіркою. Повертає slug назви, факт зайнятості та перший вільний
// варіант (та сама логіка UniqueID, що застосується при створенні).
func (a *App) CheckCustomPackID(name string) CustomPackIDCheck {
	id := custompack.SlugifyName(name)
	if id == "" {
		id = "pack"
	}
	_, exists := a.customPacks.Get(id)
	return CustomPackIDCheck{
		ID:        id,
		Exists:    exists,
		Suggested: a.customPacks.UniqueID(name),
	}
}

// ── Імпорт .mrpack / .zip (вкладка "Імпортувати") ────────────────────────

// DetectModpackArchive розпізнає обраний файл БЕЗ довантаження файлів —
// одразу після вибору файлу на вкладці "Імпортувати" показує назву,
// версію MC, лоадер, кількість модів (той самий UX, що processImportFile
// у старому лаунчері, але з реальним парсингом модрінт/curseforge
// маніфесту замість імітації).
func (a *App) DetectModpackArchive(archivePath string) (*custompack.DetectedPack, error) {
	return custompack.DetectArchive(archivePath)
}

// customImportProgressToBuildsProgress мапить custompack.ImportProgress у
// той самий builds.Progress, яким живиться картка/сторінка збірки —
// візуально користувач бачить ідентичний прогрес-бар незалежно від того,
// Shaurma-збірка якається чи власний імпортований модпак.
func customImportProgressToBuildsProgress(ip custompack.ImportProgress) *builds.Progress {
	return &builds.Progress{
		Percent:     percentOf(ip.BytesDownloaded, ip.BytesTotal, ip.FilesCompleted, ip.FilesTotal),
		BytesDone:   ip.BytesDownloaded,
		BytesTotal:  ip.BytesTotal,
		FilesDone:   ip.FilesCompleted,
		FilesTotal:  ip.FilesTotal,
		CurrentFile: ip.CurrentFile,
		SpeedBPS:    int64(ip.SpeedBPS),
		Mode:        "running",
		Stage:       ip.Stage,
	}
}

func percentOf(bytesDone, bytesTotal int64, filesDone, filesTotal int) int {
	if bytesTotal > 0 {
		return int(bytesDone * 100 / bytesTotal)
	}
	if filesTotal > 0 {
		return filesDone * 100 / filesTotal
	}
	return 0
}

// customSyncProgressPayload — рівно той JSON-контракт, що фронтендний тип
// SyncProgress (types.ts): sync:progress від internal/sync.Runner для
// Shaurma-збірок і від custompack-імпорту мають бути візуально
// невідмінні одне від одного, тому поля/назви збігаються 1:1, а не
// просто "схожі".
type customSyncProgressPayload struct {
	PackID          string  `json:"packId"`
	PackName        string  `json:"packName"`
	Mode            string  `json:"mode"`
	FilesTotal      int     `json:"filesTotal"`
	FilesCompleted  int     `json:"filesCompleted"`
	BytesTotal      int64   `json:"bytesTotal"`
	BytesDownloaded int64   `json:"bytesDownloaded"`
	OverallPercent  int     `json:"overallPercent"`
	CurrentFile     string  `json:"currentFile"`
	SpeedBps        float64 `json:"speedBps"`
	Stage           string  `json:"stage"`
	NoInternet      bool    `json:"noInternet"`
	Done            bool    `json:"done"`
	ErrorMsg        string  `json:"errorMsg,omitempty"`
	// IconURL — іконка модпаку (local-file://…), якщо вже з'явилась у
	// placeholder'а під час імпорту (вбудована icon.png). Фронтенд одразу
	// оновлює картку/сайдбар — іконка видна вже під час встановлення.
	IconURL string `json:"iconUrl,omitempty"`
}

func customImportProgressToSyncPayload(ip custompack.ImportProgress, packName string) customSyncProgressPayload {
	mode := "running"
	if ip.Error != nil {
		mode = "error"
	} else if ip.Done {
		mode = "complete"
	}
	errMsg := ""
	if ip.Error != nil {
		errMsg = ip.Error.Error()
	}
	return customSyncProgressPayload{
		PackID:          ip.PackID,
		PackName:        packName,
		Mode:            mode,
		FilesTotal:      ip.FilesTotal,
		FilesCompleted:  ip.FilesCompleted,
		BytesTotal:      ip.BytesTotal,
		BytesDownloaded: ip.BytesDownloaded,
		OverallPercent:  percentOf(ip.BytesDownloaded, ip.BytesTotal, ip.FilesCompleted, ip.FilesTotal),
		CurrentFile:     ip.CurrentFile,
		SpeedBps:        ip.SpeedBPS,
		Stage:           ip.Stage,
		Done:            ip.Done,
		ErrorMsg:        errMsg,
	}
}

// ImportModpackArchive виконує повний імпорт (розпаковка + довантаження
// модів) у фоні: одразу повертає ID нової збірки (для негайного переходу
// на її сторінку — "при створенні збірки чи імпорті переносимось на
// сторінку цієї збірки"), а прогрес транслюється подіями sync:progress /
// dlTransient так само, як для Shaurma-збірок, поки триває довантаження.
func (a *App) ImportModpackArchive(archivePath string, detected *custompack.DetectedPack, name, color, iconSrcPath, icon string) (custompack.Pack, error) {
	settings := a.cfg.GetSettings()
	packName := firstNonEmpty(name, detected.SuggestedName)

	// ID резервуємо синхронно (до довантаження файлів), щоб фронтенд міг
	// одразу перейти на сторінку збірки й побачити прогрес, що вже пишеться.
	// PreassignedID — той самий ID передається в імпорт, щоб діалог
	// заблокованих модів (import:blocked) знав, до якої збірки він.
	preID := a.customPacks.UniqueID(packName)
	ctx, cancel := context.WithCancel(context.Background())
	packIDCh := make(chan string, 1)
	resultCh := make(chan struct {
		pack custompack.Pack
		err  error
	}, 1)

	go func() {
		defer crashguard.Recover("custompack_api.go:604")
		pack, err := custompack.ImportArchive(
			ctx, a.customPacks, a.cfg.InstancesDir(), archivePath, detected,
			custompack.ImportParams{
				Name: name, Color: color, IconSrcPath: iconSrcPath, Icon: icon, ArchivePath: archivePath,
				// Заблоковані автором моди (CurseForge opt-out): діалог як у Prism.
				PreassignedID:         preID,
				BlockedHandler:        a.blockedModsHandler(preID),
				UseModrinthForBlocked: settings.UseModrinthForBlocked,
			},
			settings.MaxConcurrentDownloads,
			func(ip custompack.ImportProgress) {
				select {
				case packIDCh <- ip.PackID:
				default:
				}
				// Після паузи/скасування НЕ перезаписуємо dlTransient-стан:
				// пізній тік прогресу знову позначив би картку «running» і
				// затік стан «Пауза», який щойно виставив PauseModpackImport.
				if ctx.Err() != nil {
					return
				}
				a.recordCustomImportProgress(ip)
				payload := customImportProgressToSyncPayload(ip, packName)
				// Іконка placeholder'а могла з'явитись під час імпорту
				// (вбудована в архів icon.png) — просуваємо її в UI, щоб
				// сайдбар/картка показували іконку модпаку вже при встановленні.
				if cp, ok := a.customPacks.Get(ip.PackID); ok && cp.IconPath != "" {
					payload.IconURL = a.packAssetURL(cp, cp.IconPath)
				}
				a.emit("sync:progress", payload)
			},
		)
		resultCh <- struct {
			pack custompack.Pack
			err  error
		}{pack, err}
	}()

	// Чекаємо або перший тік прогресу (маємо ID — фронт може перейти на
	// сторінку збірки й дивитись прогрес далі через builds:changed/events),
	// або миттєве завершення (крихітний генерик-архів без файлів модів).
	select {
	case id := <-packIDCh:
		a.customImportMu.Lock()
		a.customImportCancel[id] = cancel
		a.customImportMu.Unlock()
		go func() {
			defer crashguard.Recover("custompack_api.go:651")
			res := <-resultCh
			a.customImportMu.Lock()
			delete(a.customImportCancel, id)
			a.customImportMu.Unlock()
			if res.err != nil {
				// Пауза/скасування користувачем НЕ є помилкою: стан «Пауза»/
				// «Скасовано» вже емітнув PauseModpackImport/CancelModpackImport,
				// тому download:error (тост «помилка завантаження») не показуємо.
				if ctx.Err() == nil {
					// Справжня помилка імпорту: ТЕРМІНАЛЬНА sync:progress з
					// mode="error" + errorMsg, щоб картка НЕ лишалась у стані
					// downloading назавжди. Раніше помилка емітилась лише
					// download:error, який фронт показував як "[object Object]"
					// (String(об'єкта)) — імпорт виглядав «завислим без причини».
					packName := id
					if cp, ok := a.customPacks.Get(id); ok && cp.Name != "" {
						packName = cp.Name
					}
					a.dlTransientMu.Lock()
					delete(a.dlTransient, id)
					a.dlTransientMu.Unlock()
					a.clearCustomImportState(id)
					a.emit("sync:progress", customSyncProgressPayload{
						PackID: id, PackName: packName, Mode: "error",
						Done: true, ErrorMsg: res.err.Error(),
					})
					// Локальний імпорт, що впав, — фантомний запис прибираємо
					// (як при скасуванні); браузерні збірки лишаються
					// «не завантажено» і спробу можна повторити.
					if cp, ok := a.customPacks.Get(id); ok && cp.Source == "import" {
						_ = a.customPacks.Delete(id)
					}
					a.emit("download:error", map[string]string{"packId": id, "message": res.err.Error()})
				} else {
					// Фінальне слово паузи (якщо була пауза, а не скасування):
					// пізній тік прогресу не має затерти стан «Пауза».
					a.finalizeCustomPaused(id)
				}
			} else {
				// Після успішного імпорту mrpack/zip — застосовуємо дефолтні
				// налаштування гри до options.txt (якщо функцію ввімкнено),
				// не чіпаючи решту файла. Прибираємо можливий paused-маркер
				// (імпорт завершився успішно — «Пауза» більше неактуальна).
				a.customImportMu.Lock()
				delete(a.customImportPaused, id)
				a.customImportMu.Unlock()
				// Позначені галочками моди в діалозі → Downloaded=true
				if checked := a.popBlockedCheckedMods(id); len(checked) > 0 {
					if cp, ok := a.customPacks.Get(id); ok && len(cp.BlockedMods) > 0 {
						checkedSet := make(map[string]bool, len(checked))
						for _, fn := range checked {
							checkedSet[fn] = true
						}
						changed := false
						for i := range cp.BlockedMods {
							if checkedSet[cp.BlockedMods[i].FileName] {
								cp.BlockedMods[i].Downloaded = true
								changed = true
							}
						}
						if changed {
							_ = a.customPacks.Save(cp)
						}
					}
				}
				a.applyMinecraftDefaultsNow(id)
				a.emit("builds:changed", res.pack.ID)
			}
		}()
		return custompack.Pack{ID: id, Name: firstNonEmpty(name, detected.SuggestedName)}, nil
	case res := <-resultCh:
		cancel()
		if res.err != nil {
			return custompack.Pack{}, res.err
		}
		a.applyMinecraftDefaultsNow(res.pack.ID)
		a.emit("builds:changed", res.pack.ID)
		return res.pack, nil
	}
}

// PauseModpackImport — м'яка зупинка активного імпорту модпаку (кнопка
// «Зупинити»): контекст імпорту скасовується (качання зупиняється),
// часткові файли лишаються на диску, а останній прогрес зберігається в
// dlTransient з Mode="paused" і емітиться sync:progress — фронтенд одразу
// показує на картці «Пауза» і кнопку «Продовжити» (той самий контракт,
// що для Шаурма-збірок через sync.Runner.Pause).
func (a *App) PauseModpackImport(packID string) {
	a.customImportMu.Lock()
	cancel, ok := a.customImportCancel[packID]
	if ok {
		// Мітимо як призупинений (а не скасований) — результат-горутина
		// імпорту перевірить це і виставить «Паузу» фінальним словом.
		a.customImportPaused[packID] = true
	}
	a.customImportMu.Unlock()
	if ok {
		cancel()
		a.emitCustomPaused(packID)
	}
}

// emitCustomPaused виставляє стан збірки в dlTransient з Mode="paused" і
// емітить sync:progress з mode='paused' (той самий JSON-контракт, що
// customImportProgressToSyncPayload). Якщо запису в dlTransient ще немає
// (пауза до першого тіку прогресу) — створюємо його, щоб картка одразу
// показала «Пауза»/«Продовжити», а не висіла в downloading.
func (a *App) emitCustomPaused(packID string) {
	packName := packID
	if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
		packName = cp.Name
	}
	payload := customSyncProgressPayload{PackID: packID, PackName: packName, Mode: "paused", Stage: custompack.StageDownloading}
	a.dlTransientMu.Lock()
	p, ok := a.dlTransient[packID]
	if !ok {
		p = &builds.Progress{Mode: "paused", Stage: custompack.StageDownloading}
		a.dlTransient[packID] = p
	}
	p.Mode = "paused"
	payload.FilesTotal = p.FilesTotal
	payload.FilesCompleted = p.FilesDone
	payload.BytesTotal = p.BytesTotal
	payload.BytesDownloaded = p.BytesDone
	payload.OverallPercent = p.Percent
	payload.CurrentFile = p.CurrentFile
	payload.SpeedBps = float64(p.SpeedBPS)
	payload.Stage = p.Stage
	// Персистентний стан паузи одразу у БД: рестарт лаунчера покаже на
	// картці «Пауза»/«Продовжити» з реальним % (див. customBuildView).
	a.saveCustomImportStateLocked(custompack.ImportProgress{
		PackID:          packID,
		Stage:           p.Stage,
		FilesCompleted:  p.FilesDone,
		FilesTotal:      p.FilesTotal,
		BytesDownloaded: p.BytesDone,
		BytesTotal:      p.BytesTotal,
	})
	a.refreshTaskbarProgressLocked()
	a.dlTransientMu.Unlock()
	a.emit("sync:progress", payload)
}

// finalizeCustomPaused — «останнє слово» паузи після повного завершення
// конвеєра імпорту (result-горутина): якщо імпорт був призупинений, ще раз
// виставляє стан «Пауза». Це закриває TOCTOU-гонку, коли тік прогресу,
// що встиг пройти ctx-перевірку ДО паузи, записав running у dlTransient
// вже ПІСЛЯ emitCustomPaused — фінальний re-assert перемагає.
func (a *App) finalizeCustomPaused(packID string) {
	a.customImportMu.Lock()
	paused := a.customImportPaused[packID]
	delete(a.customImportPaused, packID)
	a.customImportMu.Unlock()
	if paused {
		a.emitCustomPaused(packID)
	}
}

// CancelModpackImport скасовує активний імпорт (кнопка "Скасувати" на
// сторінці збірки, що якається з .mrpack/.zip). Емітить sync:progress з
// mode='cancelled' — фронтенд прибирає прогрес з картки (той самий шлях,
// що для Шаурма-збірок через sync.Runner.Cancel). Для модпаку, імпортова-
// ного з ЛОКАЛЬНОГО файлу (Source="import"), скасування прибирає і
// фантомний запис збірки; для браузерних «інтернет-збірок» картка
// «не завантажено» — легітимна і лишається.
func (a *App) CancelModpackImport(packID string) {
	a.cancelCustomImport(packID)
}

// cancelCustomImport — спільна частина повного скасування імпорту: кнопка
// «Скасувати» (CancelModpackImport) і «Скасувати» в діалозі заблокованих
// модів (RespondBlockedMods) роблять одне й те саме — скасовують контекст
// імпорту, прибирають dlTransient-стан, видаляють фантомний запис ЛОКАЛЬ-
// НОГО імпорту (Source=="import") і емітять термінальну sync:progress
// mode="cancelled", щоб картка не лишалась у стані downloading назавжди.
func (a *App) cancelCustomImport(packID string) {
	// Ім'я збірки читаємо ДО можливого видалення запису (інакше в події
	// cancelled замість назви був би лише ID).
	packName := packID
	if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
		packName = cp.Name
	}
	a.customImportMu.Lock()
	cancel, ok := a.customImportCancel[packID]
	delete(a.customImportPaused, packID) // скасування перемагає паузу
	a.customImportMu.Unlock()
	if ok {
		cancel()
	}
	a.dlTransientMu.Lock()
	delete(a.dlTransient, packID)
	// Таскбар перемальовуємо ПІД м'ютексом (refresh читає dlTransient).
	a.refreshTaskbarProgressLocked()
	a.dlTransientMu.Unlock()

	// Повне скасування = видалити і персистентний стан, і те, що встигло
	// скачатись (на відміну від паузи, яка лишає .part для Range-докачки).
	a.clearCustomImportState(packID)
	packDir := a.packDir(packID, "")
	cp, _ := a.customPacks.Get(packID)
	if cp.Source == "import" {
		// Локальний імпорт: збірка ще не «встановлена» — це фантомний запис
		// на час імпорту, тож повне скасування прибирає і картку, і теку
		// з частково скачаним (уже розпаковані overrides/моди теж геть).
		_ = retryRemoveAll(packDir)
		_ = a.customPacks.Delete(packID)
	} else {
		// Браузерний модпак: картка «не завантажено» лишається (її можна
		// качати заново), але часткові .part модів і .download-архів прибираємо.
		removePartFiles(filepath.Join(packDir, ".minecraft"))
		_ = os.RemoveAll(filepath.Join(packDir, ".download"))
	}
	a.emit("sync:progress", customSyncProgressPayload{PackID: packID, PackName: packName, Mode: "cancelled", Done: true})
}

// blockedModsHandler — обробник заблокованих автором модів (CurseForge
// 3rd party download opt-out), що передається в ImportArchive /
// UpdatePackFromSource. Аналог BlockedModsDialog у Prism: імпорт СТАЄ НА
// ПАУЗУ, фронтенд отримує подію import:blocked зі списком модів, показує
// діалог, і користувач обирає: Modrinth fallback / пропустити / скачати
// вручну / скасувати.
// allowModrinth=false (другий раунд після невдалого пошуку на Modrinth)
// передається у подію — діалог ховає кнопку «Завантажити з Modrinth» і
// лишає лише ручне встановлення / пропустити / скасувати.
// Очікування переривається скасуванням/паузою імпорту (ctx.Done).
func (a *App) blockedModsHandler(packID string) custompack.BlockedHandlerFunc {
	return func(ctx context.Context, blocked []custompack.BlockedCFMod, allowModrinth bool) (custompack.BlockedModsDecision, error) {
		// Канал відповіді реєструємо ДО еміту import:blocked: фронтенд може
		// відповісти одразу після отримання події, і якби реєстрація сталась
		// пізніше — відповідь була б втрачена, а імпорт чекав би рішення
		// вічно (гонка WebView-roundtrip проти кількох інструкцій тут).
		ch := make(chan custompack.BlockedModsDecision, 1)
		a.customImportMu.Lock()
		a.blockedWaiters[packID] = ch
		a.customImportMu.Unlock()
		defer func() {
			a.customImportMu.Lock()
			delete(a.blockedWaiters, packID)
			a.customImportMu.Unlock()
		}()

		// Поки імпорт чекає рішення — картка показує зрозумілий статус,
		// а не «зависле» безкінечне качання.
		packName := packID
		if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
			packName = cp.Name
		}
		a.emit("sync:progress", customSyncProgressPayload{
			PackID: packID, PackName: packName, Mode: "running",
			Stage:       custompack.StageDownloading,
			CurrentFile: "Очікуємо рішення щодо заблокованих модів…",
		})
		a.emit("import:blocked", map[string]any{"packId": packID, "mods": blocked, "allowModrinth": allowModrinth})

		select {
		case d := <-ch:
			return d, nil
		case <-ctx.Done():
			// Скасування/пауза імпорту — не чекаємо рішення далі.
			return custompack.BlockedDecisionCancel, ctx.Err()
		}
	}
}

// RespondBlockedMods — відповідь фронтенду на діалог заблокованих модів
// (подія import:blocked). decision: "modrinth" | "skip" | "manual" | "cancel".
// checkedMods — кома-розділений список назв файлів, які користувач позначив
// як «скачані вручну» (галочки в діалозі).
func (a *App) RespondBlockedMods(packID, decision string, checkedMods string) {
	// Зберігаємо позначені моди: при створенні збірки вони будуть
	// позначені як Downloaded=true.
	if checkedMods != "" {
		a.customImportMu.Lock()
		if a.blockedCheckedMods == nil {
			a.blockedCheckedMods = make(map[string][]string)
		}
		a.blockedCheckedMods[packID] = strings.Split(checkedMods, ",")
		a.customImportMu.Unlock()
	}

	a.customImportMu.Lock()
	ch, ok := a.blockedWaiters[packID]
	a.customImportMu.Unlock()
	if !ok {
		return // діалогу вже немає (імпорт завершився/скасований) — ігноруємо
	}
	// Неблокуючий send: канал буферизований(1), але якщо очікувач уже
	// завершився (ctx.Done) — не залишаємо «повідомлення в пляшці».
	sendDecision := func(d custompack.BlockedModsDecision) {
		select {
		case ch <- d:
		default:
		}
	}
	switch custompack.BlockedModsDecision(decision) {
	case custompack.BlockedDecisionModrinth, custompack.BlockedDecisionSkip, custompack.BlockedDecisionManual:
		// manual — діалог показав прямі посилання на сторінки CurseForge
		// (кнопка «Скачати вручну» відкрила їх у браузері), імпорт триває
		// БЕЗ заблокованих модів — на рівні конвеєра це той самий шлях, що skip.
		sendDecision(custompack.BlockedModsDecision(decision))
	case custompack.BlockedDecisionCancel:
		// Закриття діалогу = зберегти збірку з overrides + blocked mods
		// і НЕ качати більше нічого. resolveBlockedMods поверне пусті tasks
		// + BlockedMods → downloadImportTasks буде no-op → збірка зберігається.
		sendDecision(custompack.BlockedDecisionCancel)
	}
}

// ── Персистентний стан імпорту (Pack.ImportState) ────────────────────────
// Пауза/крах лаунчера/вимкнення ПК до повного завершення імпорту мають
// лишати збірку відновлюваною: стан зберігається у custom-packs.json (та
// сама БД збірок), «Продовжити» після рестарту докачує .part через Range,
// а вже скачані моди й розпаковані overrides пропускаються.

// customImportStateSaveInterval — мінімальний інтервал між записами стану
// імпорту у custom-packs.json. Прогрес іде тіками кожні ~150мс; писати JSON
// на кожен тік марно (і атомарний запис не безкоштовний).
const customImportStateSaveInterval = 700 * time.Millisecond

// saveCustomImportStateLocked зберігає персистентний стан імпорту (під
// dlTransientMu, з троттлінгом за customImportStateAt). Безпечно викликати
// з будь-якого місця, де вже тримається dlTransientMu.
func (a *App) saveCustomImportStateLocked(ip custompack.ImportProgress) {
	if ip.PackID == "" {
		return
	}
	a.customImportStateMu.Lock()
	if last, ok := a.customImportStateAt[ip.PackID]; ok && time.Since(last) < customImportStateSaveInterval {
		a.customImportStateMu.Unlock()
		return
	}
	a.customImportStateAt[ip.PackID] = time.Now()
	a.customImportStateMu.Unlock()

	p, ok := a.customPacks.Get(ip.PackID)
	if !ok {
		return
	}
	st := p.ImportState
	if st == nil {
		st = &custompack.ImportState{}
	}
	st.Stage = ip.Stage
	st.FilesCompleted = ip.FilesCompleted
	st.FilesTotal = ip.FilesTotal
	st.BytesDownloaded = ip.BytesDownloaded
	st.BytesTotal = ip.BytesTotal
	st.ArchivePath = p.ArchivePath
	st.UpdatedAt = time.Now()
	p.ImportState = st
	_ = a.customPacks.Save(p)
}

// saveCustomImportState — те саме, але для виклику БЕЗ захопленого
// dlTransientMu (сама бере м'ютекс).
func (a *App) saveCustomImportState(ip custompack.ImportProgress) {
	a.dlTransientMu.Lock()
	defer a.dlTransientMu.Unlock()
	a.saveCustomImportStateLocked(ip)
}

// clearCustomImportState прибирає персистентний стан імпорту з запису
// збірки (успішне завершення, помилка або повне скасування).
func (a *App) clearCustomImportState(packID string) {
	if packID == "" {
		return
	}
	a.customImportStateMu.Lock()
	delete(a.customImportStateAt, packID)
	a.customImportStateMu.Unlock()
	p, ok := a.customPacks.Get(packID)
	if !ok || p.ImportState == nil {
		return
	}
	p.ImportState = nil
	_ = a.customPacks.Save(p)
}

// removePartFiles рекурсивно видаляє *.part файли в теці — частково скачані
// моди при імпорті. Використовується при ПОВНОМУ скасуванні імпорту: пауза
// лишає їх для Range-докачки, скасування прибирає «те, що скачало».
func removePartFiles(dir string) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".part") {
			_ = os.Remove(path)
		}
		return nil
	})
}

// recordCustomImportProgress — аналог recordDownloadProgress/
// recordSyncProgress для конвеєра імпорту кастомних збірок: той самий
// dlTransient-канал живить sidebar, картку в "Моїх збірках" і сторінку
// збірки. При завершенні (Done без помилки) реєструє встановлену версію
// в builds.Registry, щоб збірка одразу показувала стан "Готова". Кожен тік
// також (з троттлінгом) зберігає ImportState у БД і оновлює таскбар.
func (a *App) recordCustomImportProgress(ip custompack.ImportProgress) {
	a.dlTransientMu.Lock()
	defer a.dlTransientMu.Unlock()

	if ip.Error != nil {
		delete(a.dlTransient, ip.PackID)
		a.clearCustomImportState(ip.PackID)
		a.refreshTaskbarProgressLocked()
		return
	}
	if ip.Done {
		delete(a.dlTransient, ip.PackID)
		a.buildsReg.SetInstalled(ip.PackID, builds.KindCustom, "")
		// Успішне завершення — стан більше не потрібен.
		a.clearCustomImportState(ip.PackID)
		a.refreshTaskbarProgressLocked()
		return
	}
	a.dlTransient[ip.PackID] = customImportProgressToBuildsProgress(ip)
	a.saveCustomImportStateLocked(ip)
	a.refreshTaskbarProgressLocked()
}

// ChangeCustomPackID змінює ID (і відповідно теку) кастомної збірки.
// Захист від колізій:
//   - newID не може збігатися з ID БУДЬ-ЯКОЇ іншої збірки (custompack + звичайні)
//   - newID == oldID → no-op (не помилка, просто нічого не робимо)
//   - newID генерується з назви через SlugifyName і перевіряється на вільність
//
// Після успіху емітує "builds:changed" з новим ID.
func (a *App) ChangeCustomPackID(oldID, newID string) (custompack.Pack, error) {
	newID = strings.TrimSpace(newID)
	if newID == "" {
		return custompack.Pack{}, fmt.Errorf("ID не може бути порожнім")
	}
	if newID == oldID {
		// no-op
		p, ok := a.customPacks.Get(oldID)
		if !ok {
			return custompack.Pack{}, fmt.Errorf("збірку %q не знайдено", oldID)
		}
		return p, nil
	}
	// Перевіряємо чи newID не зайнятий жодною збіркою
	if a.buildsReg.Exists(newID) {
		return custompack.Pack{}, fmt.Errorf("ID %q вже використовується іншою збіркою", newID)
	}
	// Перейменування теки на Windows падає з "Access is denied", якщо збірка
	// зараз запущена (Minecraft тримає файли/теку відкритими). Такого стану
	// не виправити ретраями — одразу відмовляємось із зрозумілою причиною,
	// щоб користувач зупинив гру.
	if a.customPackIsRunning(oldID) {
		return custompack.Pack{}, fmt.Errorf("збірка «%s» зараз запущена — зупиніть її, щоб змінити ID", oldID)
	}
	instancesDir := a.cfg.GetSettings().InstanceDir
	pack, err := a.renameCustomPackWithRetry(instancesDir, oldID, newID)
	if err != nil {
		return custompack.Pack{}, err
	}
	// Оновлюємо всі супутні стани, що посилаються на oldID — щоб перейменована
	// збірка лишилась РІВНО там, де була (як у Prism: rename не рухає збірку
	// з папки/позиції/закріплення). Без цього папка «губила» збірку, порядок
	// і закріплення втрачались, а в сайдбарі лишався «фантом» зі старим ID
	// (баг: «збірка змінила id, але створюються 2 копії — одна старим id»).
	a.buildsReg.RenameID(oldID, newID)
	a.cfg.RenameInstanceConfig(oldID, newID)
	a.migratePackIDReferences(oldID, newID)
	// ВАЖЛИВО: НЕ емітимо builds:changed/builds:removed тут. Фронтенд
	// (PackBasicSection.savePackId → onPackRenamed) СИНХРОННО мігрує свої
	// локальні копії папок/порядку зі старого ID на новий і лише ПОТІМ
	// перечитує builds (onUpdated → refreshBuilds). Якщо емітити builds:changed
	// тут (до повернення RPC), фронтенд-обробник оновить builds МИТТЄВО —
	// поки локальні packFolders ще зі старим ID. Тоді $effect очищення папок
	// побачить «збірка зі старим ID більше не існує» і ВИКИНЕ її з папки,
	// зберігши зіпсований стан (збірка випадає з папки + фантом зі старим ID).
	return pack, nil
}

// customPackIsRunning — чи запущена збірка (гра/лаунчер ще тримають теку)?
// Дивимось у реєстр запущених ігор (running-games.json) і перевіряємо, що
// PID справді живий. Перед фактичним перейменуванням ця перевірка рятує від
// "Access is denied" через заблоковану Windows-теку.
func (a *App) customPackIsRunning(buildID string) bool {
	if buildID == "" {
		return false
	}
	a.runningGamesMu.Lock()
	list := a.readRunningGamesLocked()
	a.runningGamesMu.Unlock()
	for _, e := range list {
		if e.BuildID == buildID && e.PID > 0 && isProcessAlive(e.PID) {
			return true
		}
	}
	return false
}

// renameCustomPackWithRetry перейменовує теку збірки, повторюючи спробу з
// невеликою паузою. На Windows os.Rename теки інколи падає з "Access is
// denied" через ТРАНЗИТНІ блокування (Провідник тримає теку робочою,
// індексатор/антивирус читає файли): такі замки швидко звільняються, тож
// одного повернення помилки недостатньо — пробуємо ще кілька разів.
// Стежимо, щоб успіх чи фінальна помилка були для користувача зрозумілими.
func (a *App) renameCustomPackWithRetry(instancesDir, oldID, newID string) (custompack.Pack, error) {
	// Store.Rename рухає теку ПЕРШИМ кроком і при невдачі повертає помилку
	// ДО оновлення реєстру, тож повторний виклик безпечний (стан не зіпсується).
	const attempts = 5
	var lastErr error
	for i := 0; i < attempts; i++ {
		pack, err := a.customPacks.Rename(instancesDir, oldID, newID)
		if err == nil {
			return pack, nil
		}
		lastErr = err
		// Ретраїмо лише проблему з переміщенням теки; інші помилки
		// (немає збірки, зайнятий ID) фінальні.
		if !strings.Contains(err.Error(), "переміщення теки") {
			return custompack.Pack{}, err
		}
		time.Sleep(time.Duration(150*(i+1)) * time.Millisecond)
	}
	if lastErr != nil {
		return custompack.Pack{}, fmt.Errorf("%w (переконайтеся, що збірка не запущена, і повторіть)", lastErr)
	}
	return custompack.Pack{}, fmt.Errorf("переміщення теки не вдалось")
}

// migratePackIDReferences переносить ВСІ локальні стани, що посилаються на
// ID збірки, з oldID на newID при перейменуванні (ChangeCustomPackID):
//   - PackFolders (папки кастомних збірок) — PackIds містять oldID;
//   - PackOrder / PackPinned (порядок і закріплення сайдбара);
//   - PinnedServerOrder (порядок закріплених серверів, ключі "packId|addr");
//   - playtime (ігровий час, ключ instance_id);
//   - ярлик збірки (.lnk з --launch <oldID> → перестворюється з новим ID).
func (a *App) migratePackIDReferences(oldID, newID string) {
	s := a.cfg.GetSettings()
	dirty := false

	// Папки: замінюємо oldID на newID у PackIds (збірка лишається у своїй
	// папці, а не «випадає» з неї і не плодить фантом у списку папки).
	for i := range s.PackFolders {
		for j, pid := range s.PackFolders[i].PackIds {
			if pid == oldID {
				s.PackFolders[i].PackIds[j] = newID
				dirty = true
			}
		}
	}

	// Порядок і закріплення сайдбара: та сама позиція, те саме закріплення.
	if listHasID(s.PackOrder, oldID) {
		s.PackOrder = replaceIDInList(s.PackOrder, oldID, newID)
		dirty = true
	}
	if listHasID(s.PackPinned, oldID) {
		s.PackPinned = replaceIDInList(s.PackPinned, oldID, newID)
		dirty = true
	}

	// Порядок закріплених серверів: ключі "<packId>|<address>" — міняємо
	// префікс packId.
	prefix := oldID + "|"
	for i, k := range s.PinnedServerOrder {
		if strings.HasPrefix(k, prefix) {
			s.PinnedServerOrder[i] = newID + strings.TrimPrefix(k, oldID)
			dirty = true
		}
	}

	if dirty {
		_ = a.cfg.UpdateSettings(s)
	}

	// Ігровий час (SQLite, ключ instance_id): переносимо накопичені секунди.
	if st := a.cfg.Store(); st != nil {
		_ = st.RenamePlaytime(oldID, newID)
	}

	// Ярлик збірки: .lnk все ще вказує на --launch <oldID> — перестворюємо
	// його на місці з новим ID, зберігаючи персональні опції (акаунт/
	// автоприєднання).
	_ = a.RenameShortcut(oldID, newID)
}

// replaceIDInList повертає копію списку з усіма входженнями oldID,
// заміненими на newID (без зміни позицій).
func replaceIDInList(list []string, oldID, newID string) []string {
	out := make([]string, 0, len(list))
	changed := false
	for _, id := range list {
		if id == oldID {
			out = append(out, newID)
			changed = true
		} else {
			out = append(out, id)
		}
	}
	if !changed {
		return list
	}
	return out
}

// listHasID повідомляє, чи містить список вказаний ID (для dirty-прапора
// збереження settings перед заміною).
func listHasID(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// CheckCustomPackIDChange перевіряє новий ID збірки при редагуванні.
// На відміну від CheckCustomPackID (для створення), тут oldID ігнорується
// при перевірці — щоб збірка не "бачила" себе як конфлікт.
func (a *App) CheckCustomPackIDChange(oldID, newID string) CustomPackIDCheck {
	id := custompack.SlugifyName(newID)
	if id == "" {
		return CustomPackIDCheck{ID: "", Exists: false, Suggested: ""}
	}
	// Збірка з таким ID вже є І це не та сама збірка
	exists := a.buildsReg.Exists(id) && id != oldID
	suggested := id
	if exists {
		suggested = custompack.UniqueIDPackage(a.buildsReg.AllIDs(), id)
	}
	return CustomPackIDCheck{ID: id, Exists: exists, Suggested: suggested}
}
