package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ── Менеджер контенту збірки (ресурспаки/датапаки/шейдери) ───────────────
// Моди мають власний розгалужений менеджер (mods_api.go). Для решти типів
// контенту (resourcepacks / shaderpacks / datapacks) тут — «легкий»
// менеджер, як у Modrinth app і Prism:
//   · список встановлених файлів у теці .minecraft/<contentDir>/;
//   · увімкнення/вимкнення через суфікс .disabled (Minecraft ігнорує такі
//     файли/теки в усіх чотирьох теках — той самий механізм, що Prism);
//   · видалення;
//   · перенесення ОДНОГО файла в іншу збірку (та сама тека);
//   · відкриття теки в Провіднику.
//
// contentDir — підтека .minecraft: mods | resourcepacks | shaderpacks |
// datapacks (ті самі, що використовує ContentBrowser при встановленні).

var validContentDirs = map[string]bool{
	"mods": true, "resourcepacks": true, "shaderpacks": true, "datapacks": true,
}

const contentDisabledSuffix = ".disabled"

// errPackRunning — уніфікована перевірка «збірка зараз запущена» для
// операцій над контентом (моди/ресурспаки/шейдери/датапаки/версія).
// Під час запущеної гри змінювати файли контенту НЕ можна: Minecraft
// тримає їх відкритими, тож видалення/заміна впаде з "used by another
// process" (Windows), а зміна версії лоадера/гри зламає запущену сесію.
// Замість незрозумілої OS-помилки віддаємо зрозуміле повідомлення —
// фронтенд показує його користувачу у модальному вікні ДО спроби дії.
func (a *App) errPackRunning(packID string) error {
	if s := a.sessionsM.RunningOnBuild(packID); s != nil {
		return fmt.Errorf("збірка зараз запущена (акаунт %s). Закрийте гру і спробуйте ще раз", s.AccountName)
	}
	return nil
}

// isContentArchive — чи файл є архівом контенту (.zip/.jar; .disabled-суфікс
// попередньо зрізається). У теках resourcepacks/shaderpacks/datapacks
// Minecraft вантажить ЛИШЕ архіви та розпаковані теки — налаштування й
// нотатки (*.txt, *.json, *.mcmeta тощо) гра ігнорує, тож і в списку
// встановленого контенту їм не місце (скарга: «у шейдерах показуються
// txt-файли налаштувань»). Моди мають власний менеджер (.jar) — їх не чіпаємо.
func isContentArchive(fileName string) bool {
	base := strings.TrimSuffix(fileName, contentDisabledSuffix)
	switch strings.ToLower(filepath.Ext(base)) {
	case ".zip", ".jar":
		return true
	}
	return false
}

func (a *App) instanceContentDir(packID, contentDir string) string {
	return filepath.Join(a.gameDir(packID), contentDir)
}

func (a *App) contentDirErr(contentDir string) error {
	return fmt.Errorf("невідома тека контенту: %s (очікується mods/resourcepacks/shaderpacks/datapacks)", contentDir)
}

// instanceExists перевіряє, що збірка з таким ID існує на диску.
func (a *App) instanceExists(packID string) bool {
	if packID == "" {
		return false
	}
	dir := a.packDir(packID, "")
	_, err := os.Stat(dir)
	return err == nil
}

// ScanContentFiles повертає список встановленого контенту в теці
// .minecraft/<contentDir>/ збірки. Вимкнені файли (.disabled) позначаються
// Enabled=false, але лишаються у списку — як у менеджері модів.
// Датапаки-теки враховуються (IsDir=true).
func (a *App) ScanContentFiles(packID, contentDir string) ([]model.ContentFileEntry, error) {
	if !validContentDirs[contentDir] {
		return nil, a.contentDirErr(contentDir)
	}
	dir := a.instanceContentDir(packID, contentDir)
	if !a.instanceExists(packID) {
		// Збірка ще не встановлена — це не помилка, а «контенту немає».
		return []model.ContentFileEntry{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []model.ContentFileEntry{}, nil
		}
		return nil, err
	}
	out := make([]model.ContentFileEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// Ховаємо системні/тимчасові файли та бекапи менеджера модів.
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
			continue
		}
		// Для немод-контенту (resourcepacks/shaderpacks/datapacks) лишаємо
		// лише архіви (.zip/.jar) і теки: Minecraft з цих тек вантажить саме
		// їх, а txt-налаштування/нотатки не повинні показуватись як
		// «встановлений ресурс» (і діставати toggle/перенос/видалення).
		if contentDir != "mods" && !e.IsDir() && !isContentArchive(name) {
			continue
		}
		enabled := true
		fileName := name
		if strings.HasSuffix(name, contentDisabledSuffix) {
			enabled = false
			fileName = strings.TrimSuffix(name, contentDisabledSuffix)
		}
		display := fileName
		ext := filepath.Ext(fileName)
		if ext != "" && !e.IsDir() {
			display = strings.TrimSuffix(fileName, ext)
		}
		out = append(out, model.ContentFileEntry{
			FileName: name,
			Name:     display,
			Enabled:  enabled,
			IsDir:    e.IsDir(),
		})
	}
	// Сортуємо: увімкнені зверху, далі за ім'ям.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && contentLess(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// contentLess — порядок: увімкнені перед вимкненими, далі за ім'ям (без урахування регістру).
func contentLess(x, y model.ContentFileEntry) bool {
	if x.Enabled != y.Enabled {
		return x.Enabled
	}
	return strings.ToLower(x.Name) < strings.ToLower(y.Name)
}

// ToggleContentFile увімкнює/вимикає файл контенту: додає/прибирає суфікс
// .disabled (Minecraft ігнорує такі файли та теки у всіх content-теках).
func (a *App) ToggleContentFile(packID, contentDir, fileName string, enabled bool) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	// Увімкнення/вимкнення контенту дозволено і під час запущеної гри:
	// це лише перейменування у .disabled (Minecraft ігнорує такі файли при
	// наступному старті, запущену сесію не ламає). Решта мутацій
	// (delete/update/версія) перевіряють errPackRunning нижче.
	fileName = filepath.Base(fileName) // захист від path traversal
	dir := a.instanceContentDir(packID, contentDir)
	path := filepath.Join(dir, fileName)

	// Для модів лишаємо спеціалізований шлях (у нього своя логіка/валідація).
	if contentDir == "mods" {
		return a.ToggleMod(packID, fileName, enabled)
	}
	a.invalidateContentResolve(packID, contentDir, fileName)

	twin := func(base string) string { return base + contentDisabledSuffix }

	if enabled {
		// Увімкнення: прибираємо .disabled. Приймаємо і базове ім'я, і вже
		// з суфіксом — idempotent в обидва боки.
		base := strings.TrimSuffix(fileName, contentDisabledSuffix)
		if _, err := os.Stat(filepath.Join(dir, base)); err == nil {
			return nil // вже увімкнений
		}
		disabledPath := filepath.Join(dir, base+contentDisabledSuffix)
		if _, err := os.Stat(disabledPath); err != nil {
			return fmt.Errorf("файл не знайдено: %s", fileName)
		}
		return os.Rename(disabledPath, filepath.Join(dir, base))
	}

	// Вимкнення: додаємо .disabled. Якщо файл вже вимкнений (як за базовим
	// ім'ям, так і за .disabled) — нічого не робимо.
	if strings.HasSuffix(fileName, contentDisabledSuffix) {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("файл не знайдено: %s", fileName)
		}
		return nil // вже вимкнений
	}
	if _, err := os.Stat(path); err != nil {
		// Можливо, вже вимкнений під ім'ям з .disabled (передали базове).
		if _, terr := os.Stat(twin(path)); terr == nil {
			return nil
		}
		return fmt.Errorf("файл не знайдено: %s", fileName)
	}
	return os.Rename(path, twin(path))
}

// DeleteContentFile видаляє файл контенту (і його .disabled-двійник, якщо є).
func (a *App) DeleteContentFile(packID, contentDir, fileName string) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	if err := a.errPackRunning(packID); err != nil {
		return err
	}
	fileName = filepath.Base(fileName)
	dir := a.instanceContentDir(packID, contentDir)

	if contentDir == "mods" {
		return a.DeleteMod(packID, fileName)
	}
	a.invalidateContentResolve(packID, contentDir, fileName)

	// Видаляємо і сам файл, і його .disabled-двійник (якщо він є) —
	// незалежно від того, який з них переданий.
	paths := []string{filepath.Join(dir, fileName)}
	if strings.HasSuffix(fileName, contentDisabledSuffix) {
		paths = append(paths, filepath.Join(dir, strings.TrimSuffix(fileName, contentDisabledSuffix)))
	} else {
		paths = append(paths, filepath.Join(dir, fileName+contentDisabledSuffix))
	}
	var firstErr error
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// TransferContentFile переносить ОДИН ресурс у іншу збірку: копіює файл
// (або теку) у ту саму теку .minecraft/<contentDir>/ цільової збірки,
// зберігаючи стан увімкнення (.disabled-суфікс). Джерело НЕ видаляється —
// це копіювання (як «встановити в іншу збірку» у Modrinth app). При
// overwrite=false і вже наявному файлі — помилка (фронтенд питає підтвердження).
func (a *App) TransferContentFile(packID, contentDir, fileName, targetPackID string, overwrite bool) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	// Блокуємо запис і в джерело, і в ціль: копіювання НА запущену збірку
	// теж неприпустиме (файл може підхопитись грою на льоту), а з
	// запущеної — файл відкритий Minecraft-ом.
	if err := a.errPackRunning(packID); err != nil {
		return err
	}
	if err := a.errPackRunning(targetPackID); err != nil {
		return err
	}
	fileName = filepath.Base(fileName)
	if !a.instanceExists(packID) {
		return fmt.Errorf("збірку не знайдено: %s", packID)
	}
	if !a.instanceExists(targetPackID) {
		return fmt.Errorf("цільову збірку не знайдено: %s", targetPackID)
	}
	if packID == targetPackID {
		return fmt.Errorf("не можна перенести в ту саму збірку")
	}
	src := filepath.Join(a.instanceContentDir(packID, contentDir), fileName)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("файл не знайдено: %s", fileName)
	}
	dstDir := a.instanceContentDir(targetPackID, contentDir)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dst := filepath.Join(dstDir, fileName)
	if _, err := os.Stat(dst); err == nil && !overwrite {
		return fmt.Errorf("у збірці «%s» уже є файл %s", targetPackID, fileName)
	}
	if err := copyContent(src, dst); err != nil {
		return err
	}
	return nil
}

// copyContent копіює файл АБО теку (для датапаків-тек) зі збереженням
// прав. Ціль попередньо видаляється (при overwrite).
func copyContent(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

// OpenContentFolder відкриває теку .minecraft/<contentDir>/ збірки в
// системному файловому менеджері (кнопка «Відкрити теку» в UI).
func (a *App) OpenContentFolder(packID, contentDir string) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	dir := a.instanceContentDir(packID, contentDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("не вдалось створити теку: %v", err)
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return xdgOpen(dir)
}

// contentFileSHA1 — SHA1 файлу (для майбутнього зіставлення з Modrinth/CF).
func contentFileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ── Версії та оновлення контенту (Modrinth-рівень, як у менеджері модів) ──
// Для ресурспаків/датапаків/шейдерів підтягуємо з Modrinth таку саму
// інформацію, що менеджер модів показує для модів: поточна встановлена
// версія, наявність оновлення, список версій («Змінити версію») і
// «Перевірити оновлення». Лоадера у цих типів немає — його не фільтруємо.
//
// Моди цей блок не чіпають — у них власний розгалужений менеджер.

// contentResolveEntry — кешований результат зіставлення встановленого
// файла контенту з проєктом Modrinth (по SHA1). Кешуємо в пам'яті, щоб
// не бити /version_file/{hash} на кожне відкриття під-вкладки.
type contentResolveEntry struct {
	at            time.Time
	Sha1          string
	ProjectID     string
	Slug          string
	Source        string
	Version       string
	LatestVersion string
	HasUpdate     bool
	Author        string
	IconURL       string
}

var (
	contentResolveMu sync.Mutex
	contentResolve   = map[string]contentResolveEntry{}
)

const contentResolveTTL = 15 * time.Minute

// contentResolveKey — ключ кешу: збірка+тека+файл+SHA1 (при зміні версії
// файла змінюється й SHA1 — кеш автоматично нівелюється).
func contentResolveKey(packID, contentDir, fileName, sha1 string) string {
	return packID + "|" + contentDir + "|" + fileName + "|" + sha1
}

func (a *App) invalidateContentResolve(packID, contentDir, fileName string) {
	contentResolveMu.Lock()
	defer contentResolveMu.Unlock()
	// Видаляємо всі записи збірки+теки+файла (SHA1 може бути будь-який).
	for k := range contentResolve {
		if strings.HasPrefix(k, packID+"|"+contentDir+"|"+fileName+"|") {
			delete(contentResolve, k)
		}
	}
}

// resolveContentEntry знаходить встановлений файл контенту на Modrinth за
// SHA1 і повертає версійні поля. Для тек (datapack-каталогів) — нічого
// (SHA1 на теку порахувати не можна як у файла).
func (a *App) resolveContentEntry(packID, contentDir, fileName, mcVersion string) (contentResolveEntry, bool) {
	res := contentResolveEntry{}
	dir := a.instanceContentDir(packID, contentDir)
	cleanName := strings.TrimSuffix(fileName, contentDisabledSuffix)
	path := filepath.Join(dir, cleanName)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return res, false
	}
	sha1, err := contentFileSHA1(path)
	if err != nil || sha1 == "" {
		return res, false
	}
	key := contentResolveKey(packID, contentDir, fileName, sha1)
	contentResolveMu.Lock()
	if e, ok := contentResolve[key]; ok && time.Since(e.at) < contentResolveTTL {
		contentResolveMu.Unlock()
		return e, true
	}
	contentResolveMu.Unlock()

	v, err := a.modrinth.GetVersionByHash(sha1)
	if err != nil || v == nil || v.ProjectID == "" {
		return res, false
	}
	res.Sha1 = sha1
	res.ProjectID = v.ProjectID
	res.Source = "modrinth"
	res.Version = v.VersionNumber
	if len(v.Files) > 0 {
		res.Slug = v.Files[v.PrimaryFileIndex()].Filename
	}
	// Найсвіжіша версія під поточну версію гри (для badge «оновлення»).
	if latest, lerr := a.modrinth.LatestVersionFor(v.ProjectID, mcVersion, "", a.cfg.GetSettings().AllowAlphaBetaModUpdates); lerr == nil && latest != nil {
		normCur := normalizeDepName(res.Version)
		normLat := normalizeDepName(latest.VersionNumber)
		res.LatestVersion = latest.VersionNumber
		res.HasUpdate = normLat != "" && normCur != "" && normLat != normCur
	}
	if p, perr := a.modrinth.GetProject(v.ProjectID); perr == nil && p != nil {
		res.Author = p.Title
		res.IconURL = p.IconURL
	}

	contentResolveMu.Lock()
	contentResolve[key] = res
	contentResolveMu.Unlock()
	return res, true
}

// RefreshContentUpdates сканує файли контенту в теці .minecraft/<contentDir>/
// і підтягує для кожного з Modrinth версійні дані (поточна версія, чи є
// оновлення, автор, іконка). Це аналог перевірки оновлень модів: викликаємо
// після збереження зміни версії гри або вручну, і список одразу показує
// badge «оновлення».
func (a *App) RefreshContentUpdates(packID, contentDir, mcVersion string) ([]model.ContentFileEntry, error) {
	files, err := a.ScanContentFiles(packID, contentDir)
	if err != nil {
		return nil, err
	}
	for i := range files {
		if files[i].IsDir || contentDir == "mods" {
			continue
		}
		if info, ok := a.resolveContentEntry(packID, contentDir, files[i].FileName, mcVersion); ok {
			files[i].Sha1 = info.Sha1
			files[i].ProjectID = info.ProjectID
			files[i].Slug = info.Slug
			files[i].Source = info.Source
			files[i].Version = info.Version
			files[i].LatestVersion = info.LatestVersion
			files[i].HasUpdate = info.HasUpdate
			files[i].Author = info.Author
			files[i].IconURL = info.IconURL
		}
	}
	return files, nil
}

// GetContentVersions повертає список доступних версій встановленого
// ресурсу (модалка «Змінити версію»). Спершу шукає проєкт по SHA1 файла,
// потім список його версій під версію гри збірки. Поточна версія
// позначається IsCurrent.
func (a *App) GetContentVersions(packID, contentDir, fileName, mcVersion string) []model.ModVersionOption {
	files, err := a.ScanContentFiles(packID, contentDir)
	if err != nil {
		return nil
	}
	var cur *model.ContentFileEntry
	for i := range files {
		if files[i].FileName == fileName {
			cur = &files[i]
			break
		}
	}
	if cur == nil {
		return nil
	}
	res, ok := a.resolveContentEntry(packID, contentDir, fileName, mcVersion)
	if !ok || res.ProjectID == "" {
		return nil
	}
	opts, err := a.modrinth.AllVersionsFor(res.ProjectID, mcVersion, "")
	if err != nil {
		return nil
	}
	// Позначаємо поточну: за sha1, точним ім'ям файла або номером версії.
	baseName := strings.TrimSuffix(fileName, contentDisabledSuffix)
	for i := range opts {
		if res.Sha1 != "" && opts[i].Sha1 != "" && strings.EqualFold(opts[i].Sha1, res.Sha1) {
			opts[i].IsCurrent = true
		} else if opts[i].Filename != "" && strings.EqualFold(opts[i].Filename, baseName) {
			opts[i].IsCurrent = true
		} else if res.Version != "" && normalizeDepName(opts[i].VersionNumber) == normalizeDepName(res.Version) {
			opts[i].IsCurrent = true
		}
	}
	return opts
}

// InstallContentVersion завантажує обрану версію ресурсу і замінює нею
// поточний файл, зберігаючи стан «вимкнено» (.disabled). Використовує
// той самий механізм завантаження з ретраями, що й менеджер модів.
func (a *App) InstallContentVersion(packID, contentDir, fileName string, opt model.ModVersionOption) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	if err := a.errPackRunning(packID); err != nil {
		return err
	}
	if contentDir == "mods" {
		return a.InstallModVersion(packID, fileName, opt)
	}
	if opt.URL == "" || opt.Filename == "" {
		return fmt.Errorf("немає посилання на файл версії")
	}
	dir := a.instanceContentDir(packID, contentDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	// Завантажуємо у тимчасовий файл (щоб частково скачаний архів не
	// почав вантажитися Minecraft-ом як частково дійсний ресурспак).
	opt.Filename = safeModFileName(opt.Filename)
	tmp := filepath.Join(dir, "."+opt.Filename+".part")
	if err := a.downloadModFile(opt.URL, tmp); err != nil {
		return err
	}
	disabled := strings.HasSuffix(fileName, contentDisabledSuffix)
	// Старий файл: видаляємо і звичайне ім'я, і .disabled-двійник.
	_ = os.Remove(filepath.Join(dir, fileName))
	_ = os.Remove(filepath.Join(dir, strings.TrimSuffix(fileName, contentDisabledSuffix)))
	newName := opt.Filename
	if disabled {
		newName = opt.Filename + contentDisabledSuffix
	}
	if err := os.Rename(tmp, filepath.Join(dir, newName)); err != nil {
		os.Remove(tmp)
		return err
	}
	a.invalidateContentResolve(packID, contentDir, newName)
	a.invalidateContentResolve(packID, contentDir, fileName)
	return nil
}

// GetContentFileLink повертає URL сторінки ресурсу на Modrinth (для
// «Скопіювати посилання» у контекстному меню).
func (a *App) GetContentFileLink(packID, contentDir, fileName, mcVersion string) string {
	if info, ok := a.resolveContentEntry(packID, contentDir, fileName, mcVersion); ok {
		if info.Source == "modrinth" && info.ProjectID != "" {
			if info.Slug != "" {
				return "https://modrinth.com/" + contentProjectType(contentDir) + "/" + info.Slug
			}
			return "https://modrinth.com/" + contentProjectType(contentDir) + "/" + info.ProjectID
		}
	}
	return ""
}

// contentProjectType — Modrinth-тип проєкту для теки контенту.
func contentProjectType(contentDir string) string {
	switch contentDir {
	case "resourcepacks":
		return "resourcepack"
	case "datapacks":
		return "datapack"
	case "shaderpacks":
		return "shader"
	}
	return ""
}

// PickContentFiles відкриває системний діалог вибору файлів контенту
// (resourcepacks/datapacks/shaderpacks) — .zip файли.
func (a *App) PickContentFiles(contentDir string) ([]string, error) {
	label := "Оберіть файли"
	switch contentDir {
	case "resourcepacks":
		label = "Оберіть ресурспаки (.zip)"
	case "datapacks":
		label = "Оберіть датапаки (.zip)"
	case "shaderpacks":
		label = "Оберіть шейдери (.zip)"
	}
	files := bridgeDialogOpenFiles(label, map[string][]string{
		"Pack files (*.zip)": {"zip"},
	})
	return files, nil
}

// ImportContentFiles копіює файли (.zip) у теку контенту збірки.
func (a *App) ImportContentFiles(packID, contentDir string, paths []string) (ModImportResult, error) {
	res := ModImportResult{Imported: []string{}, Skipped: []string{}, Failed: []string{}}
	if !validContentDirs[contentDir] {
		return res, a.contentDirErr(contentDir)
	}
	if err := a.errPackRunning(packID); err != nil {
		return res, err
	}
	dir := a.instanceContentDir(packID, contentDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return res, err
	}
	for _, src := range paths {
		ext := strings.ToLower(filepath.Ext(src))
		if ext != ".zip" && ext != ".jar" {
			res.Skipped = append(res.Skipped, filepath.Base(src))
			continue
		}
		name := filepath.Base(src)
		dst := filepath.Join(dir, name)
		// Унікалізуємо ім'я при колізії
		base := strings.TrimSuffix(name, ext)
		for i := 2; ; i++ {
			_, e1 := os.Stat(dst)
			_, e2 := os.Stat(dst + ".disabled")
			if os.IsNotExist(e1) && os.IsNotExist(e2) {
				break
			}
			dst = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		}
		if err := copyFile(src, dst); err != nil {
			res.Failed = append(res.Failed, filepath.Base(src))
		} else {
			res.Imported = append(res.Imported, filepath.Base(dst))
		}
	}
	return res, nil
}

// CheckContentInBuilds перевіряє наявність файлу у всіх збірках.
// Повертає карту buildID → bool (true = файл вже є).
func (a *App) CheckContentInBuilds(fileName, contentDir string) map[string]bool {
	result := map[string]bool{}
	views := a.GetBuilds()
	for _, v := range views {
		id := v.Build.ID
		dir := a.instanceContentDir(id, contentDir)
		base := strings.TrimSuffix(fileName, ".disabled")
		_, e1 := os.Stat(filepath.Join(dir, base))
		_, e2 := os.Stat(filepath.Join(dir, base+".disabled"))
		result[id] = !os.IsNotExist(e1) || !os.IsNotExist(e2)
	}
	return result
}

// SmartCopyContentFile розумно копіює ресурс в іншу збірку:
// якщо є сумісна версія на Modrinth — завантажує її,
// інакше просто копіює файл.
func (a *App) SmartCopyContentFile(srcPackID, contentDir, fileName, targetPackID, targetMcVersion string) error {
	if !validContentDirs[contentDir] {
		return a.contentDirErr(contentDir)
	}
	if err := a.errPackRunning(srcPackID); err != nil {
		return err
	}
	if err := a.errPackRunning(targetPackID); err != nil {
		return err
	}
	if srcPackID == targetPackID {
		return fmt.Errorf("не можна копіювати в ту саму збірку")
	}
	// Пробуємо знайти сумісну версію для цільової збірки
	versions := a.GetContentVersions(srcPackID, contentDir, fileName, targetMcVersion)
	if len(versions) > 0 {
		targetDir := a.instanceContentDir(targetPackID, contentDir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}
		v := versions[0]
		dst := filepath.Join(targetDir, safeModFileName(v.Filename))
		return downloadFileTo(v.URL, dst)
	}
	// Fallback: просто копіюємо файл як є
	return a.TransferContentFile(srcPackID, contentDir, fileName, targetPackID, false)
}
