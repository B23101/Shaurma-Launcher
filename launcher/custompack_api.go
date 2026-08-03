package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"shaurma-launcher-wails/internal/builds"
	"shaurma-launcher-wails/internal/custompack"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ── Версії Minecraft / лоадерів (форма "Нова збірка → Вручну") ──────────

// GetCustomPackMCVersions повертає список версій Minecraft для селектора
// версії гри — найновіші перші, як віддає Mojang.
func (a *App) GetCustomPackMCVersions() ([]custompack.MCVersionEntry, error) {
	return a.versionsProv.GetMCVersions()
}

// GetCustomPackLoaderVersions повертає версії конкретного лоадера,
// сумісні з обраною версією Minecraft (перша в списку = рекомендована).
// loader: "vanilla" | "fabric" | "forge" | "neoforge" | "quilt".
func (a *App) GetCustomPackLoaderVersions(loader, mcVersion string) ([]custompack.LoaderVersionEntry, error) {
	return a.versionsProv.GetLoaderVersions(custompack.Loader(loader), mcVersion)
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

// BrowseImageFile відкриває діалог вибору іконки/фону збірки (PNG/JPG/WEBP).
func (a *App) BrowseImageFile() (string, error) {
	if app := application.Get(); app != nil {
		return app.Dialog.OpenFile().
			SetTitle("Виберіть зображення").
			AddFilter("Зображення (*.png;*.jpg;*.jpeg;*.webp)", "*.png;*.jpg;*.jpeg;*.webp").
			PromptForSingleSelection()
	}
	return "", fmt.Errorf("no context")
}

// BrowseModpackArchive відкриває діалог вибору файлу модпаку для імпорту
// (.mrpack / .zip) — вкладка "Імпортувати".
func (a *App) BrowseModpackArchive() (string, error) {
	if app := application.Get(); app != nil {
		return app.Dialog.OpenFile().
			SetTitle("Виберіть файл модпаку").
			AddFilter("Модпаки (*.mrpack;*.zip)", "*.mrpack;*.zip").
			PromptForSingleSelection()
	}
	return "", fmt.Errorf("no context")
}

// ── Створення / редагування / видалення кастомної збірки (вручну) ───────

// CustomPackFormParams — плаский набір полів форми, зручний для виклику з
// фронтенду (уникає вкладених типів у Wails-бінденні). Дзеркалить
// custompack.CreateParams.
type CustomPackFormParams struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	IconSrcPath        string `json:"iconSrcPath"`
	BackgroundSrcPath  string `json:"backgroundSrcPath"`
	Icon               string `json:"icon"` // пресет-іконка (Tabler, напр. "ti-puzzle")
	Color              string `json:"color"`
	MCVersion          string `json:"mcVersion"`
	Loader             string `json:"loader"`
	LoaderVersion      string `json:"loaderVersion"`
	UseCustomRAM       bool   `json:"useCustomRam"`
	MinRAMMB           int    `json:"minRamMb"`
	MaxRAMMB           int    `json:"maxRamMb"`
	UseSeparateJava    bool   `json:"useSeparateJava"`
	JavaPath           string `json:"javaPath"`
}

func (p CustomPackFormParams) toCreateParams() custompack.CreateParams {
	return custompack.CreateParams{
		Name:              p.Name,
		Description:       p.Description,
		IconSrcPath:       p.IconSrcPath,
		BackgroundSrcPath: p.BackgroundSrcPath,
		Icon:              p.Icon,
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
	settings := a.cfg.GetSettings()
	pack, err := custompack.Create(a.customPacks, settings.InstanceDir, p.toCreateParams())
	if err != nil {
		return custompack.Pack{}, err
	}
	// Ручне створення = збірка "просто створена і завантажена": без цього
	// картка показувала б "Завантажити"/вікно прогресу, як для Shaurma-пака,
	// що збивало б з пантелику (див. customBuildView: !installed → not-installed).
	a.buildsReg.SetInstalled(pack.ID, builds.KindCustom, "")
	a.emit("builds:changed", pack.ID)
	return pack, nil
}

// UpdateCustomPack редагує вже створену кастомну збірку.
func (a *App) UpdateCustomPack(id string, p CustomPackFormParams) (custompack.Pack, error) {
	old, _ := a.customPacks.Get(id)
	settings := a.cfg.GetSettings()
	pack, err := custompack.Update(a.customPacks, settings.InstanceDir, id, p.toCreateParams())
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
	return p, nil
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
	PackID          string `json:"packId"`
	PackName        string `json:"packName"`
	Mode            string `json:"mode"`
	FilesTotal      int    `json:"filesTotal"`
	FilesCompleted  int    `json:"filesCompleted"`
	BytesTotal      int64  `json:"bytesTotal"`
	BytesDownloaded int64  `json:"bytesDownloaded"`
	OverallPercent  int    `json:"overallPercent"`
	CurrentFile     string `json:"currentFile"`
	SpeedBps        float64 `json:"speedBps"`
	Stage           string `json:"stage"`
	NoInternet      bool   `json:"noInternet"`
	Done            bool   `json:"done"`
	ErrorMsg        string `json:"errorMsg,omitempty"`
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
	ctx, cancel := context.WithCancel(context.Background())
	packIDCh := make(chan string, 1)
	resultCh := make(chan struct {
		pack custompack.Pack
		err  error
	}, 1)

	go func() {
		pack, err := custompack.ImportArchive(
			ctx, a.customPacks, settings.InstanceDir, archivePath, detected,
			custompack.ImportParams{Name: name, Color: color, IconSrcPath: iconSrcPath, Icon: icon},
			settings.MaxConcurrentDownloads,
			func(ip custompack.ImportProgress) {
				select {
				case packIDCh <- ip.PackID:
				default:
				}
				a.recordCustomImportProgress(ip)
				select {
				case <-ctx.Done():
				default:
					payload := customImportProgressToSyncPayload(ip, packName)
					// Іконка placeholder'а могла з'явитись під час імпорту
					// (вбудована в архів icon.png) — просуваємо її в UI, щоб
					// сайдбар/картка показували іконку модпаку вже при встановленні.
					if cp, ok := a.customPacks.Get(ip.PackID); ok && cp.IconPath != "" {
						payload.IconURL = "local-file://" + cp.IconPath
					}
					a.emit("sync:progress", payload)
				}
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
			res := <-resultCh
			a.customImportMu.Lock()
			delete(a.customImportCancel, id)
			a.customImportMu.Unlock()
			if res.err != nil {
				a.emit("download:error", map[string]string{"packId": id, "message": res.err.Error()})
			} else {
				a.emit("builds:changed", res.pack.ID)
			}
		}()
		return custompack.Pack{ID: id, Name: firstNonEmpty(name, detected.SuggestedName)}, nil
	case res := <-resultCh:
		cancel()
		if res.err != nil {
			return custompack.Pack{}, res.err
		}
		a.emit("builds:changed", res.pack.ID)
		return res.pack, nil
	}
}

// CancelModpackImport скасовує активний імпорт (кнопка "Скасувати" на
// сторінці збірки, що якається з .mrpack/.zip). Часткові файли лишаються
// на диску (та сама поведінка "Pause", не повне видалення) — користувач
// може видалити збірку окремою дією, якщо не хоче лишати частковий стан.
func (a *App) CancelModpackImport(packID string) {
	a.customImportMu.Lock()
	cancel, ok := a.customImportCancel[packID]
	a.customImportMu.Unlock()
	if ok {
		cancel()
	}
	a.dlTransientMu.Lock()
	delete(a.dlTransient, packID)
	a.dlTransientMu.Unlock()
}

// recordCustomImportProgress — аналог recordDownloadProgress/
// recordSyncProgress для конвеєра імпорту кастомних збірок: той самий
// dlTransient-канал живить sidebar, картку в "Моїх збірках" і сторінку
// збірки. При завершенні (Done без помилки) реєструє встановлену версію
// в builds.Registry, щоб збірка одразу показувала стан "Готова".
func (a *App) recordCustomImportProgress(ip custompack.ImportProgress) {
	a.dlTransientMu.Lock()
	defer a.dlTransientMu.Unlock()

	if ip.Error != nil {
		delete(a.dlTransient, ip.PackID)
		return
	}
	if ip.Done {
		delete(a.dlTransient, ip.PackID)
		a.buildsReg.SetInstalled(ip.PackID, builds.KindCustom, "")
		return
	}
	a.dlTransient[ip.PackID] = customImportProgressToBuildsProgress(ip)
}
