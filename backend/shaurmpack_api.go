package main

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
	"shaurma-launcher-backend/internal/shaurmpack"
)

// ── .shaurmpack — власний формат модпаків (import/export) ──────────────
// Окремий шлях від ImportModpackArchive/ExportPack (.mrpack/.zip/curseforge):
// той конвеєр (custompack.ImportArchive) заточений під чужі формати й має
// власну чергу/паузу/резюм, тут — простіший прямий виклик
// internal/shaurmpack.Export/Import (уже виправлений: Modrinth → CurseForge,
// заблоковані автором CF-моди йдуть у bundled/ без спроби довантаження —
// див. internal/shaurmpack/pack.go, unpack.go).

// ShaurmpackExtensions — розширення, які розпізнає вкладка «Імпортувати»
// і файловий діалог нижче.
const (
	shaurmpackExt   = ".shaurmpack"
	shaurmbundleExt = ".shaurmbundle"
)

// BrowseShaurmpackFile відкриває діалог вибору .shaurmpack/.shaurmbundle —
// окремий фільтр від BrowseModpackArchive (mrpack/zip), щоб не змішувати
// формати в одному діалозі.
func (a *App) BrowseShaurmpackFile() (string, error) {
	return bridgeDialogOpenFile("Виберіть Shaurma-збірку", false, map[string][]string{
		"Shaurma-збірки (*.shaurmpack;*.shaurmbundle)": {"shaurmpack", "shaurmbundle"},
	}), nil
}

// ShaurmpackPreview — те, що показується на прев'ю-екрані вкладки
// "Імпортувати" одразу після вибору .shaurmpack файлу, ще до самого
// імпорту. Дзеркалить shaurmpack.Manifest у плаский, зручний для Wails-
// серіалізації вигляд (той самий підхід, що DetectedPack для mrpack/zip).
type ShaurmpackPreview struct {
	PackID        string   `json:"packId"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	VersionID     string   `json:"versionId"`
	Authors       []string `json:"authors,omitempty"`
	GameVersion   string   `json:"gameVersion"`
	Loader        string   `json:"loader"`
	LoaderVersion string   `json:"loaderVersion,omitempty"`
	Color         string   `json:"color,omitempty"`
	FileCount     int      `json:"fileCount"`
	BundledCount  int      `json:"bundledCount"`
	ResolvedCount int      `json:"resolvedCount"`
	LauncherName  string   `json:"launcherName,omitempty"`
	// IconDataURL — вбудована іконка збірки (icon.png у корені архіву) як
	// data URL для прев'ю у формі імпорту. Порожній, якщо іконки немає.
	IconDataURL string `json:"iconDataUrl,omitempty"`
}

// PreviewShaurmpack читає manifest.json з .shaurmpack БЕЗ розпаковування —
// швидка операція для прев'ю (аналог DetectModpackArchive).
func (a *App) PreviewShaurmpack(path string) (*ShaurmpackPreview, error) {
	m, err := shaurmpack.ReadManifest(path)
	if err != nil {
		return nil, err
	}
	if err := shaurmpack.ValidateManifest(m); err != nil {
		return nil, fmt.Errorf("некоректний .shaurmpack: %w", err)
	}
	preview := &ShaurmpackPreview{
		PackID:        m.PackID,
		Name:          m.Name,
		Description:   m.Description,
		VersionID:     m.VersionID,
		Authors:       m.Authors,
		GameVersion:   m.GameVersion,
		Loader:        m.Loader,
		LoaderVersion: m.LoaderVersion,
		Color:         m.Color,
		FileCount:     len(m.Files),
		BundledCount:  m.BundledCount,
		ResolvedCount: shaurmpackResolvedCount(m),
		LauncherName:  m.Launcher.Name,
	}
	// Вбудована іконка збірки — для прев'ю у формі імпорту (та сама
	// поведінка, що mrpack/zip через DetectedPack.iconDataUrl).
	if data := readShaurmpackRootIcon(path); data != nil {
		preview.IconDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	}
	return preview, nil
}

// shaurmpackResolvedCount — скільки файлів паку мають онлайн-джерело (качаються
// при імпорті). Нові паки пишуть resolvedCount; для старих рахуємо з Files.
func shaurmpackResolvedCount(m *shaurmpack.Manifest) int {
	if m.ResolvedCount > 0 {
		return m.ResolvedCount
	}
	n := 0
	for _, f := range m.Files {
		if !f.Bundled && f.HasWorkingSource() {
			n++
		}
	}
	return n
}

// registerShaurmpackPlaceholder створює теку збірки й РЕЄСТРУЄ збірку-
// заготовку у сховищі (щоб вона одразу з'явилась у списку, поки йде
// розпаковування й завантаження). Спільна частина синхронного імпорту
// (ImportShaurmpackFile) і фонового (StartShaurmpackImport). Повертає
// заготовку та шлях до .minecraft. Якщо збірку створено, а далі щось
// пішло не так — прибирає її викликач (customPacks.Delete).
func (a *App) registerShaurmpackPlaceholder(path, name, color, iconSrcPath, icon string, m *shaurmpack.Manifest) (custompack.Pack, string, error) {
	packName := strings.TrimSpace(name)
	if packName == "" {
		packName = m.Name
	}
	id := a.customPacks.UniqueID(packName)
	packDir := filepath.Join(a.cfg.InstancesDir(), id)
	gameDir := filepath.Join(packDir, ".minecraft")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return custompack.Pack{}, "", fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}
	config.MarkNotIndexed(packDir)
	config.MarkNotIndexed(gameDir)

	placeholder := custompack.Pack{
		ID:            id,
		Name:          packName,
		Description:   m.Description,
		Color:         firstNonEmpty(color, m.Color),
		MCVersion:     m.GameVersion,
		Loader:        m.Loader,
		LoaderVersion: m.LoaderVersion,
		Source:        "import",
		ImportFile:    filepath.Base(path),
		ArchivePath:   path,
	}
	if iconSrcPath != "" {
		placeholder.IconPath = iconSrcPath
	}
	if icon != "" {
		placeholder.Icon = icon
	}
	// Персистентність: оригінальна іконка (icon.png у корені архіву)
	// відновлюється в теці збірки. Пріоритет: явно обраний PNG
	// (iconSrcPath) > пресет (icon) > вбудована іконка архіву.
	if placeholder.IconPath == "" && icon == "" {
		if data := readShaurmpackRootIcon(path); data != nil {
			if err := os.WriteFile(filepath.Join(packDir, "icon.png"), data, 0644); err == nil {
				placeholder.IconPath = "icon.png"
			}
		}
	}
	// Вітання з personalization.json (якщо є).
	if p, perr := shaurmpack.ReadPersonalization(path); perr == nil && p != nil {
		if p.WelcomeMessage != "" && placeholder.Description == "" {
			placeholder.Description = p.WelcomeMessage
		}
	}
	if err := a.customPacks.Save(placeholder); err != nil {
		_ = os.RemoveAll(packDir)
		return custompack.Pack{}, "", fmt.Errorf("не вдалося зареєструвати збірку: %w", err)
	}
	return placeholder, gameDir, nil
}

// ImportShaurmpackFile імпортує .shaurmpack у нову кастомну збірку —
// СИНХРОННО (вкладка «Імпортувати»: виклик повертається, коли все готово).
// Для двічі-кліку по файлу і взагалі «як модпак з браузера» — з прогресом
// на картці збірки — див. StartShaurmpackImport.
func (a *App) ImportShaurmpackFile(path, name, color, iconSrcPath, icon string) (custompack.Pack, error) {
	if !strings.EqualFold(filepath.Ext(path), shaurmpackExt) {
		return custompack.Pack{}, fmt.Errorf("очікувався файл %s", shaurmpackExt)
	}
	m, err := shaurmpack.ReadManifest(path)
	if err != nil {
		return custompack.Pack{}, err
	}
	if err := shaurmpack.ValidateManifest(m); err != nil {
		return custompack.Pack{}, fmt.Errorf("некоректний .shaurmpack: %w", err)
	}

	placeholder, gameDir, err := a.registerShaurmpackPlaceholder(path, name, color, iconSrcPath, icon, m)
	if err != nil {
		return custompack.Pack{}, err
	}
	id := placeholder.ID
	committed := false
	defer func() {
		if !committed {
			_ = a.customPacks.Delete(id)
		}
	}()

	result, err := shaurmpack.Import(shaurmpack.ImportOptions{
		ShaurmPackPath:  path,
		TargetGameDir:   gameDir,
		ResolveDownload: a.shaurmpackDownloadResolver(),
	})
	if err != nil {
		return custompack.Pack{}, fmt.Errorf("імпорт .shaurmpack: %w", err)
	}

	pack, ok := a.customPacks.Get(id)
	if !ok {
		pack = placeholder
	}
	committed = true
	a.buildsReg.SetInstalled(id, builds.KindCustom, m.VersionID)
	a.emit("builds:changed", id)
	a.emit("shaurmpack:import:done", map[string]any{
		"packId":      id,
		"filesTotal":  result.FilesTotal + result.FilesCopied,
		"filesCopied": result.FilesCopied,
		"filesLinked": result.FilesLinked,
	})
	return pack, nil
}

// StartShaurmpackImport — ФОНОВИЙ імпорт .shaurmpack «як модпак з браузера»
// (двічі клік по файлу в Провіднику): одразу повертає нову збірку (фронтенд
// переходить на її сторінку), а розпакування й завантаження модів іде у
// фоні з прогресом через sync:progress — та сама картка/прогрес-бар, паузa/
// скасування й таскбар-прогрес, що й в ImportModpackArchive. Жодної модалки.
func (a *App) StartShaurmpackImport(path string) (custompack.Pack, error) {
	if !strings.EqualFold(filepath.Ext(path), shaurmpackExt) {
		return custompack.Pack{}, fmt.Errorf("очікувався файл %s", shaurmpackExt)
	}
	m, err := shaurmpack.ReadManifest(path)
	if err != nil {
		return custompack.Pack{}, err
	}
	if err := shaurmpack.ValidateManifest(m); err != nil {
		return custompack.Pack{}, fmt.Errorf("некоректний .shaurmpack: %w", err)
	}
	placeholder, gameDir, err := a.registerShaurmpackPlaceholder(path, "", "", "", "", m)
	if err != nil {
		return custompack.Pack{}, err
	}
	a.emit("builds:changed", placeholder.ID)
	a.startShaurmpackImportRun(placeholder, gameDir, m, false)
	return placeholder, nil
}

// resumeShaurmpackImport — «Продовжити» після паузи для збірки, імпортованої
// з .shaurmpack (див. ResumeModpackImport): повторний прохід з того самого
// файлу; уже завантажені цілі файли пропускаються.
func (a *App) resumeShaurmpackImport(pack custompack.Pack) (custompack.Pack, error) {
	m, err := shaurmpack.ReadManifest(pack.ArchivePath)
	if err != nil {
		return custompack.Pack{}, fmt.Errorf("не вдалося відкрити %s: %w", pack.ArchivePath, err)
	}
	gameDir := filepath.Join(a.cfg.InstancesDir(), pack.ID, ".minecraft")
	a.startShaurmpackImportRun(pack, gameDir, m, true)
	return custompack.Pack{ID: pack.ID, Name: pack.Name}, nil
}

// startShaurmpackImportRun реєструє скасування й запускає імпорт у горутині.
func (a *App) startShaurmpackImportRun(pack custompack.Pack, gameDir string, m *shaurmpack.Manifest, resume bool) {
	id := pack.ID
	ctx, cancel := context.WithCancel(context.Background())
	a.customImportMu.Lock()
	a.customImportCancel[id] = cancel
	delete(a.customImportPaused, id)
	a.customImportMu.Unlock()

	// Перша подія одразу — картка показує «Розпаковуємо…» без затримки.
	a.reportShaurmpackProgress(ctx, pack, shaurmpack.ImportProgress{Stage: shaurmpack.ImportStageExtracting})

	go func() {
		defer crashguard.Recover("shaurmpack_api.go:startShaurmpackImportRun")
		defer func() {
			cancel()
			a.customImportMu.Lock()
			delete(a.customImportCancel, id)
			a.customImportMu.Unlock()
		}()

		result, err := shaurmpack.Import(shaurmpack.ImportOptions{
			ShaurmPackPath:  pack.ArchivePath,
			TargetGameDir:   gameDir,
			ResolveDownload: a.shaurmpackDownloadResolver(),
			Context:         ctx,
			SkipExisting:    resume,
			Progress: func(p shaurmpack.ImportProgress) {
				a.reportShaurmpackProgress(ctx, pack, p)
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				// Пауза або скасування користувачем — не помилка. Скасування
				// вже повністю прибрало збірку (cancelCustomImport); для
				// паузи виставляємо «останнє слово» стану «Пауза».
				a.finalizeCustomPaused(id)
				return
			}
			a.failShaurmpackImport(pack, err)
			return
		}

		a.dlTransientMu.Lock()
		delete(a.dlTransient, id)
		a.refreshTaskbarProgressLocked()
		a.dlTransientMu.Unlock()
		a.clearCustomImportState(id)
		a.buildsReg.SetInstalled(id, builds.KindCustom, m.VersionID)
		a.emit("sync:progress", customSyncProgressPayload{
			PackID: id, PackName: pack.Name, Mode: "complete", Stage: custompack.StageComplete,
			FilesTotal: result.FilesTotal + result.FilesCopied, FilesCompleted: result.FilesTotal + result.FilesCopied,
			OverallPercent: 100, Done: true,
		})
		a.applyMinecraftDefaultsNow(id)
		a.emit("builds:changed", id)
		a.emit("shaurmpack:import:done", map[string]any{
			"packId":      id,
			"filesTotal":  result.FilesTotal + result.FilesCopied,
			"filesCopied": result.FilesCopied,
			"filesLinked": result.FilesLinked,
		})
	}()
}

// reportShaurmpackProgress транслює прогрес імпорту в UI тими самими
// каналами, що й custompack-імпорт: dlTransient (картка, таскбар) +
// sync:progress. Після паузи/скасування (ctx) нічого не пише — пізній тік
// не має затерти стан «Пауза».
func (a *App) reportShaurmpackProgress(ctx context.Context, pack custompack.Pack, p shaurmpack.ImportProgress) {
	if ctx.Err() != nil {
		return
	}
	ip := custompack.ImportProgress{
		PackID: pack.ID, FilesTotal: p.FilesTotal, FilesCompleted: p.FilesDone,
		CurrentFile: p.CurrentFile, Stage: p.Stage,
	}
	a.dlTransientMu.Lock()
	a.dlTransient[pack.ID] = customImportProgressToBuildsProgress(ip)
	a.refreshTaskbarProgressLocked()
	a.dlTransientMu.Unlock()
	payload := customImportProgressToSyncPayload(ip, pack.Name)
	if cp, ok := a.customPacks.Get(pack.ID); ok && cp.IconPath != "" {
		payload.IconURL = a.packAssetURL(cp, cp.IconPath)
	}
	a.emit("sync:progress", payload)
}

// failShaurmpackImport — справжня помилка фонового імпорту: термінальна
// sync:progress (картка не лишається в downloading), фантомну збірку з
// теками прибираємо (як для локального імпорту модпаку), помилка йде в
// download:error (тост).
func (a *App) failShaurmpackImport(pack custompack.Pack, err error) {
	id := pack.ID
	a.dlTransientMu.Lock()
	delete(a.dlTransient, id)
	a.refreshTaskbarProgressLocked()
	a.dlTransientMu.Unlock()
	a.clearCustomImportState(id)
	a.emit("sync:progress", customSyncProgressPayload{
		PackID: id, PackName: pack.Name, Mode: "error", Done: true, ErrorMsg: err.Error(),
	})
	if cp, ok := a.customPacks.Get(id); ok && cp.Source == "import" {
		_ = retryRemoveAll(a.packDir(id, ""))
		_ = a.customPacks.Delete(id)
	}
	a.emit("builds:changed", id)
	a.emit("download:error", map[string]string{"packId": id, "message": err.Error()})
}

// shaurmpackDownloadResolver повертає функцію довантаження resolved-файлів
// (Modrinth/CurseForge) для shaurmpack.Import — прямі URL уже лежать у
// маніфесті (записані при Export), тож тут просто якаємо по DownloadURL і
// нічого не резолвимо заново. Заблоковані CurseForge-файли (Blocked=true)
// НІКОЛИ сюди не доходять — internal/shaurmpack сам відсіює їх на bundled-
// шлях ще до виклику цього резолвера (guard у unpack.go).
func (a *App) shaurmpackDownloadResolver() func(destPath string, pf shaurmpack.PackFile) error {
	return func(destPath string, pf shaurmpack.PackFile) error {
		var url string
		switch {
		case pf.Modrinth != nil && pf.Modrinth.DownloadURL != "":
			url = pf.Modrinth.DownloadURL
		case pf.CurseForge != nil && !pf.CurseForge.Blocked && pf.CurseForge.DownloadURL != "":
			url = pf.CurseForge.DownloadURL
		default:
			return fmt.Errorf("немає робочого джерела для %s", pf.Path)
		}
		return shaurmpackDownloadToFile(url, destPath)
	}
}

// shaurmpackDownloadToFile — просте (без резюму/ретраїв — bundled fallback
// у shaurmpack.Import уже покриває збій) завантаження файлу за прямим URL.
// Той самий заголовок x-api-key для CurseForge CDN, що й у
// custompack.downloadOneAttempt (приватна, тому не використовується напряму
// — тут навмисно спрощена копія того самого протоколу запиту, оскільки
// bundled fallback уже страхує від помилок мережі; повний ретрай/резюм
// доречний для великих модпаків з чергою файлів, тут резолв послідовний).
func shaurmpackDownloadToFile(url, destPath string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (+shaurmpack-import; shaurmaofficial2210@gmail.com)")
	if custompack.IsCurseForgeCDNURL(url) && custompack.HasCurseForgeAPIKey() {
		req.Header.Set("x-api-key", custompack.CurseForgeAPIKeyValue())
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d при завантаженні %s", resp.StatusCode, url)
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	tmp := destPath + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destPath)
}

// readShaurmpackRootIcon читає вбудовану іконку збірки (icon.png у корені
// .shaurmpack-архіву — конвенція експорту через ExportOptions.IconFile).
// Повертає байти або nil, якщо іконки немає/завелика (>2MB).
func readShaurmpackRootIcon(path string) []byte {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != "icon.png" {
			continue
		}
		if f.FileInfo().IsDir() || f.UncompressedSize64 == 0 || f.UncompressedSize64 > 2*1024*1024 {
			return nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil || len(data) == 0 {
			return nil
		}
		return data
	}
	return nil
}

// ── Batch-резолвери для ExportShaurmpack/ExportShaurmbundle ─────────────
// internal/shaurmpack навмисно не залежить від internal/api чи
// internal/custompack — САМІ мережеві виклики робить цей шар. Резолвери
// викликаються по одному разу на весь експорт з унікальними хешами:
// Modrinth — POST /version_files (чанки по 200 хешів усередині клієнта),
// CurseForge — POST /fingerprints, далі GetModFile лише там, де потрібні
// DownloadURL/Blocked (файлу немає на Modrinth), паралельно (ліміт 10;
// частоту стримує rate-limiter клієнта CF).

// shaurmpackModrinthBatchResolver — ResolveModrinthBatch для shaurmpack.
func (a *App) shaurmpackModrinthBatchResolver() shaurmpack.ModrinthBatchResolver {
	return func(ctx context.Context, sha1s []string) map[string]*shaurmpack.ModrinthSource {
		out := make(map[string]*shaurmpack.ModrinthSource, len(sha1s))
		if len(sha1s) == 0 || ctx.Err() != nil {
			return out
		}
		versions, err := a.modrinth.GetVersionsByHashes(sha1s)
		if err != nil {
			return out // мережа/API недоступні → файли підуть у bundled/
		}
		for h, v := range versions {
			if v == nil {
				continue
			}
			// Файл версії з ТИМ САМИМ хешем (версія може мати кілька файлів);
			// інакше — primary / перший.
			idx := -1
			for i, f := range v.Files {
				if strings.EqualFold(f.Sha1, h) {
					idx = i
					break
				}
			}
			if idx < 0 {
				for i, f := range v.Files {
					if f.Primary {
						idx = i
						break
					}
				}
			}
			if idx < 0 && len(v.Files) > 0 {
				idx = 0
			}
			if idx < 0 || v.Files[idx].URL == "" {
				continue
			}
			out[h] = &shaurmpack.ModrinthSource{
				ProjectID:   v.ProjectID,
				VersionID:   v.ID,
				FileName:    v.Files[idx].Filename,
				DownloadURL: v.Files[idx].URL,
				Required:    true,
			}
		}
		return out
	}
}

// shaurmpackCurseForgeBatchResolver — ResolveCurseForgeBatch для shaurmpack.
// CurseForge НЕ шукає за SHA-1 (лише Murmur2 fingerprint, який рахує
// shaurmpack-пул воркерів), тому: fingerprints → (projectId, fileId), а
// потім GetModFile (DownloadURL, Blocked) лише для CFLookup.NeedDetails.
// Шукає за fingerprint, тож знаходить файли в будь-яких підтеках
// (mods/sub, resourcepacks, shaderpacks).
func (a *App) shaurmpackCurseForgeBatchResolver() shaurmpack.CurseForgeBatchResolver {
	cf := a.cfClient()
	return func(ctx context.Context, files []shaurmpack.CFLookup) map[string]*shaurmpack.CurseForgeSource {
		out := make(map[string]*shaurmpack.CurseForgeSource, len(files))
		if len(files) == 0 || ctx.Err() != nil {
			return out
		}
		const chunk = 500
		matches := map[string]custompack.CFFingerprintMatch{}
		for start := 0; start < len(files); start += chunk {
			end := start + chunk
			if end > len(files) {
				end = len(files)
			}
			fps := make([]string, 0, end-start)
			for _, f := range files[start:end] {
				fps = append(fps, f.Fingerprint)
			}
			m, err := cf.ResolveFingerprints(fps)
			if err != nil {
				return out // немає ключа/API → без CF-джерел
			}
			for k, v := range m {
				matches[k] = v
			}
		}

		type pending struct {
			lookup shaurmpack.CFLookup
			match  custompack.CFFingerprintMatch
		}
		var need []pending
		for _, f := range files {
			m, ok := matches[f.Fingerprint]
			if !ok {
				continue
			}
			if !f.NeedDetails {
				// Файл уже має робоче Modrinth-джерело: лише ідентифікатори
				// для атрибуції, без запиту GetModFile.
				out[f.SHA1] = &shaurmpack.CurseForgeSource{
					ProjectID: m.ProjectID, FileID: m.FileID, FileName: m.FileName, Required: true,
				}
				continue
			}
			need = append(need, pending{f, m})
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 10)
		for _, p := range need {
			if ctx.Err() != nil {
				break
			}
			wg.Add(1)
			sem <- struct{}{}
			go func(p pending) {
				defer wg.Done()
				defer func() { <-sem }()
				info, err := cf.GetModFileCtx(ctx, p.match.ProjectID, p.match.FileID)
				if err != nil || info == nil {
					// Деталі не отримано — краще не писати джерело взагалі
					// (файл піде в bundled/), ніж неповне/хибне.
					return
				}
				mu.Lock()
				out[p.lookup.SHA1] = &shaurmpack.CurseForgeSource{
					ProjectID:   p.match.ProjectID,
					FileID:      p.match.FileID,
					FileName:    p.match.FileName,
					DownloadURL: info.DownloadURL, // порожній, якщо Blocked
					Required:    true,
					Blocked:     info.Blocked,
				}
				mu.Unlock()
			}(p)
		}
		wg.Wait()
		return out
	}
}

// shaurmpackSHA1Of — кешований SHA-1 для експорту: для файлів у mods/
// використовує персистентний кеш модів лаунчера (за розміром+mtime), для
// решти повертає "" (shaurmpack порахує сам — щоб не засмічувати службову
// теку кешу файлами для довільних тек).
func (a *App) shaurmpackSHA1Of() func(absPath string) (string, error) {
	return func(absPath string) (string, error) {
		if a.modMgr == nil || filepath.Base(filepath.Dir(absPath)) != "mods" {
			return "", nil
		}
		return a.modMgr.Sha1Of(absPath), nil // "" → shaurmpack порахує сам
	}
}

// ExportShaurmpackRequest — параметри експорту в .shaurmpack з фронтенду.
//
// Included/Excluded — ТОЙ САМИЙ вибір папок/файлів, що й у mrpack-експорті
// (ExportPackRequest). Раніше цих полів тут не було взагалі, тож модалка
// експорту малювала дерево вибору, а .shaurmpack усе одно пакував УСЮ теку
// збірки — саме скарга «я вибрав mods, а він пакує все».
type ExportShaurmpackRequest struct {
	PackID      string   `json:"packId"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Authors     []string `json:"authors,omitempty"`
	OutDir      string   `json:"outDir"`
	Included    []string `json:"included"`
	Excluded    []string `json:"excluded"`
	Force       bool     `json:"force"`
}

// ExportShaurmpackResult — результат експорту в .shaurmpack.
type ExportShaurmpackResult struct {
	OutPath       string `json:"outPath"`
	Conflict      bool   `json:"conflict,omitempty"`
	TotalFiles    int    `json:"totalFiles"`
	BundledFiles  int    `json:"bundledFiles"`
	ResolvedFiles int    `json:"resolvedFiles"`
}

// ExportShaurmpack експортує кастомну збірку у .shaurmpack (аналог
// ExportPack для .mrpack/.zip/curseforge, окремий метод — інший формат,
// інший результат). Той самий заборонний виняток для офіційних Shaurma-
// збірок, що і в ExportPack (errExportForbidden) — на бекенді, не лише в
// UI, щоб пряме звернення до методу теж не дозволяло обійти обмеження.
func (a *App) ExportShaurmpack(req ExportShaurmpackRequest) (ExportShaurmpackResult, error) {
	build, err := a.resolveLaunchBuild(req.PackID)
	if err != nil {
		return ExportShaurmpackResult{}, errExportNotFound
	}
	if build.IsShaurma {
		return ExportShaurmpackResult{}, errExportForbidden
	}
	gameDir := a.packRootDir(req.PackID)
	if gameDir == "" {
		return ExportShaurmpackResult{}, errExportNotFound
	}
	if strings.TrimSpace(req.Name) == "" {
		return ExportShaurmpackResult{}, errExportName
	}
	if req.OutDir == "" {
		return ExportShaurmpackResult{}, errExportOutDir
	}
	version := req.Version
	if version == "" {
		version = "1.0.0"
	}

	outPath := filepath.Join(req.OutDir, export.SanitizeName(req.Name)+shaurmpackExt)
	if !req.Force {
		if _, err := os.Stat(outPath); err == nil {
			return ExportShaurmpackResult{OutPath: outPath, Conflict: true}, nil
		}
	}

	cp, ok := a.customPacks.Get(req.PackID)
	if !ok {
		return ExportShaurmpackResult{}, errExportNotFound
	}

	// «Єдине джерело» модів: ПЕРЕД пакуванням оновлюємо mods/ із джерел.
	if err := a.syncSourcesForExport(req.PackID); err != nil {
		return ExportShaurmpackResult{}, err
	}

	// Іконка збірки: IconPath відносний до кореня теки збірки (поруч з
	// .minecraft/) — резолвимо в абсолютний шлях для ExportOptions.IconFile.
	// Легасі-записи можуть тримати абсолютний шлях — залишаємо як є.
	packDir := a.packDir(req.PackID, "")
	var iconFile string
	if cp.IconPath != "" {
		if filepath.IsAbs(cp.IconPath) {
			iconFile = cp.IconPath
		} else {
			iconFile = filepath.Join(packDir, filepath.FromSlash(cp.IconPath))
		}
	}

	result, err := shaurmpack.Export(shaurmpack.ExportOptions{
		GameDir:           gameDir,
		IconFile:          iconFile,
		PackID:            req.PackID,
		Name:              req.Name,
		VersionID:         version,
		Description:       firstNonEmpty(req.Description, cp.Description),
		Authors:           req.Authors,
		// Вибір користувача з модалки експорту — інакше пакується вся тека.
		IncludePaths:      req.Included,
		ExcludePaths:      req.Excluded,
		GameVersion:       cp.MCVersion,
		Loader:            cp.Loader,
		LoaderVersion:     cp.LoaderVersion,
		Color:             cp.Color,
		OutPath:           outPath,
		LauncherName:      "Shaurma Launcher",
		LauncherVersion:   appVersion,
		ResolveModrinthBatch:   a.shaurmpackModrinthBatchResolver(),
		ResolveCurseForgeBatch: a.shaurmpackCurseForgeBatchResolver(),
		SHA1Of:                 a.shaurmpackSHA1Of(),
		Progress: func(done int, total int, fileName string) {
			a.emit("export:progress", map[string]any{
				"packId":   req.PackID,
				"stage":    "pack",
				"current":  done,
				"total":    total,
				"fileName": fileName,
			})
		},
	})
	if err != nil {
		return ExportShaurmpackResult{}, err
	}
	return ExportShaurmpackResult{
		OutPath:       outPath,
		TotalFiles:    result.TotalFiles,
		BundledFiles:  result.BundledFiles,
		ResolvedFiles: result.ResolvedFiles,
	}, nil
}
