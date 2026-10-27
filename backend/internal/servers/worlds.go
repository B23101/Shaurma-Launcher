// worlds.go — управління світами сервера: бекапи, відновлення, експорт/імпорт, регенерація.
package servers

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// WorldInfo — інформація про світ.
type WorldInfo struct {
	Name     string    `json:"name"`     // level-name або ім'я папки
	Path     string    `json:"path"`     // абсолютний шлях
	SizeMB   float64   `json:"sizeMB"`   // розмір
	Modified time.Time `json:"modified"` // час останньої зміни
}

// BackupInfo — інформація про бекап.
type BackupInfo struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	SizeMB  float64   `json:"sizeMB"`
	Created time.Time `json:"created"`
	World   string    `json:"world"` // який світ
	// Files — скільки файлів усередині архіву (для UI: «1 234 файли»).
	// Рахується при створенні бекапу; 0 = невідомо (старий архів).
	Files int `json:"files,omitempty"`
	// Auto — бекап створено автоматично (таймером/при зупинці), а не кнопкою.
	Auto bool `json:"auto,omitempty"`
}

// BackupProgress — стан створення бекапу для UI (прогрес-бар).
//
// Раніше створення бекапу було «чорною скринькою»: кнопка натиснута — і
// тиша, поки zip великого світу пишеться десятки секунд. Тепер бекенд шле
// цю структуру на кожному кроці (server:backup-progress), тож і ручний, і
// авто-бекап видно в інтерфейсі.
type BackupProgress struct {
	World string `json:"world"`
	// Stage: preparing (рахуємо файли) | packing (пишемо zip) | done | error.
	Stage   string `json:"stage"`
	Files   int    `json:"files"`   // усього файлів у світі
	Done    int    `json:"done"`    // уже запаковано файлів
	Percent int    `json:"percent"` // 0-100, -1 = невідомо
	Message string `json:"message"`
	Path    string `json:"path,omitempty"` // шлях готового zip
	Error   string `json:"error,omitempty"`
	// Auto — бекап створено автоматично (таймер, зупинка/старт сервера).
	Auto bool `json:"auto,omitempty"`
}

// ListWorlds повертає список світів сервера.
func ListWorlds(serverDir string) []WorldInfo {
	var worlds []WorldInfo

	// Читаємо level-name з server.properties (за замовч. "world").
	levelName := "world"
	data, err := os.ReadFile(filepath.Join(serverDir, "server.properties"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "level-name=") {
				levelName = strings.TrimPrefix(line, "level-name=")
				break
			}
		}
	}

	// Знаходимо всі папки-світи (з region/ або DIM-).
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return worlds
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		dirPath := filepath.Join(serverDir, name)
		// Перевірка чи це світ: має region/ або DIM* або level.dat
		if isWorldDir(dirPath) {
			size := dirSizeMB(dirPath)
			modTime := dirModTime(dirPath)
			worlds = append(worlds, WorldInfo{
				Name:     name,
				Path:     dirPath,
				SizeMB:   size,
				Modified: modTime,
			})
		}
	}

	// level-name першим.
	sort.Slice(worlds, func(i, j int) bool {
		if worlds[i].Name == levelName {
			return true
		}
		if worlds[j].Name == levelName {
			return false
		}
		return worlds[i].Name < worlds[j].Name
	})

	return worlds
}

// backupsDirFor — єдина точка правди для теки бекапів: <serverDir>/.shaurma/backups.
// Раніше різні виклики обчислювали її по-різному (а DeleteWorldBackup взагалі
// брав глобальну теку даних лаунчера) — через це «видалений» бекап лишався
// на місці, а відновлення шукало файл не там. Тепер шлях один для всіх.
func backupsDirFor(serverDir, backupsDir string) string {
	if strings.TrimSpace(backupsDir) != "" {
		return backupsDir
	}
	return filepath.Join(serverDir, ".shaurma", "backups")
}

// CreateBackup створює бекап світу у zip (без звітування про прогрес).
func CreateBackup(serverDir, worldName, backupsDir string) (string, error) {
	return CreateBackupWithProgress(serverDir, worldName, backupsDir, false, nil)
}

// CreateBackupWithProgress створює бекап світу у zip і, якщо передано
// onProgress, звітує про кожен крок: скільки файлів у світі, скільки вже
// запаковано, скільки відсотків. Викликається і кнопкою «Створити бекап»,
// і авто-бекапом (таймер, зупинка/старт сервера) — щоб у UI було видно
// прогрес, а не «тишу» на десятки секунд.
//
// ВАЖЛИВО про знімок «на гарячу»: якщо сервер працює, світ копіюється як є.
// Це свідомий компроміс — гра сама періодично скидає чанки на диск, а
// зупиняти сервер заради бекапу гірше. Для бекапу під час роботи гравець
// може виконати save-all у консолі; бекап при зупинці робиться ВЖЕ після
// зупинки процесу, тож там знімок консистентний.
// auto — бекап створено автоматично (таймер/зупинка/старт); прапорець іде
// і в події прогресу, і в метафайл, щоб UI позначив копію як авто.
func CreateBackupWithProgress(serverDir, worldName, backupsDir string, auto bool, onProgress func(BackupProgress)) (string, error) {
	report := func(p BackupProgress) {
		p.Auto = auto
		if onProgress != nil {
			onProgress(p)
		}
	}

	worldName = filepath.Base(strings.TrimSpace(worldName))
	if worldName == "" || worldName == "." {
		return "", fmt.Errorf("не вказано світ для бекапу")
	}
	worldDir := filepath.Join(serverDir, worldName)
	if _, err := os.Stat(worldDir); os.IsNotExist(err) {
		return "", fmt.Errorf("світ %s не знайдено", worldName)
	}

	backupsDir = backupsDirFor(serverDir, backupsDir)
	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		return "", err
	}

	// Рахуємо файли заздалегідь (один прохід) — і щоб мати знаменник для
	// відсотків, і щоб коректно показати «preparing» на великому світі.
	entries := collectZipEntries(worldDir)
	report(BackupProgress{
		World: worldName, Stage: "packing", Files: len(entries), Done: 0,
		Percent: 0, Message: fmt.Sprintf("Пакування %d файлів…", len(entries)),
	})

	// Ім'я бекапу: worldname-20260823-181035.zip
	//
	// Секундна точність означає, що два бекапи в одну секунду (напр. ручна
	// кнопка одразу після авто-бекапу) дали б ОДНЕ ім'я — і другий zip мовчки
	// перезаписав би перший. Тому додаємо числовий суфікс, поки ім'я зайняте; і
	// одночасно прибираємо старі метафайли з тим самим ID (вони б «залипли»).
	timestamp := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-%s.zip", worldName, timestamp)
	backupPath := filepath.Join(backupsDir, backupName)
	for i := 1; ; i++ {
		if _, err := os.Stat(backupPath); os.IsNotExist(err) {
			break
		}
		backupName = fmt.Sprintf("%s-%s-%d.zip", worldName, timestamp, i)
		backupPath = filepath.Join(backupsDir, backupName)
	}

	var lastPct = -1
	if err := zipEntries(worldDir, backupPath, entries, func(done, total int) {
		pct := -1
		if total > 0 {
			pct = done * 100 / total
		}
		if pct == lastPct {
			return // не спамимо подіями на кожен дрібний файл
		}
		lastPct = pct
		report(BackupProgress{
			World: worldName, Stage: "packing", Files: total, Done: done,
			Percent: pct, Message: fmt.Sprintf("Пакування… %d/%d", done, total),
		})
	}); err != nil {
		// Битий zip не має лежати в теці бекапів і вводити в оману списком.
		_ = os.Remove(backupPath)
		report(BackupProgress{World: worldName, Stage: "error", Files: len(entries), Percent: -1, Error: err.Error()})
		return "", err
	}

	backupID := strings.TrimSuffix(backupName, ".zip")
	writeBackupMeta(backupsDir, backupID, len(entries), auto)

	report(BackupProgress{
		World: worldName, Stage: "done", Files: len(entries), Done: len(entries),
		Percent: 100, Message: "Бекап створено", Path: backupPath,
	})
	return backupPath, nil
}

// ListBackups повертає список бекапів для світу.
func ListBackups(serverDir, worldName, backupsDir string) []BackupInfo {
	backupsDir = backupsDirFor(serverDir, backupsDir)

	var backups []BackupInfo
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return backups
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		name := e.Name()
		// Фільтруємо за worldName якщо вказано.
		if worldName != "" && !strings.HasPrefix(name, worldName+"-") {
			continue
		}

		info, _ := e.Info()
		sizeMB := float64(0)
		var created time.Time
		if info != nil {
			sizeMB = float64(info.Size()) / (1024 * 1024)
			created = info.ModTime()
		}

		id := strings.TrimSuffix(name, ".zip")
		bi := BackupInfo{
			ID:      id,
			Name:    name,
			SizeMB:  sizeMB,
			Created: created,
			// Світ = частина імені ДО дати: у світу може бути дефіс у назві
			// ("my-world-20260823-181035"), тож відрізаємо саме суфікс-дату,
			// а не перший дефіс.
			World: worldFromBackupID(id, worldName),
		}
		if meta, err := readBackupMeta(backupsDir, id); err == nil {
			bi.Files = meta.Files
			bi.Auto = meta.Auto
		}
		backups = append(backups, bi)
	}

	// Найновіші першими.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Created.After(backups[j].Created)
	})

	return backups
}

// backupMeta — супутній файл <id>.meta.json поруч із zip. Потрібен, щоб
// список бекапів показував кількість файлів і позначку «авто» без
// розкривання самого архіву (читати zip на кожен рендер — дорого).
type backupMeta struct {
	Files int  `json:"files"`
	Auto  bool `json:"auto"`
}

func backupMetaPath(backupsDir, backupID string) string {
	return filepath.Join(backupsDir, backupID+".meta.json")
}

func readBackupMeta(backupsDir, backupID string) (backupMeta, error) {
	var m backupMeta
	data, err := os.ReadFile(backupMetaPath(backupsDir, backupID))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return backupMeta{}, err
	}
	return m, nil
}

// writeBackupMeta — best-effort: відсутність метафайла не робить бекап
// недійсним, він лише показується без кількості файлів.
func writeBackupMeta(backupsDir, backupID string, files int, auto bool) {
	data, err := json.Marshal(backupMeta{Files: files, Auto: auto})
	if err != nil {
		return
	}
	_ = os.WriteFile(backupMetaPath(backupsDir, backupID), data, 0644)
}

// reBackupDateSuffix — хвіст імені бекапу: дата-час ("20060102-150405") плюс
// необов'язковий числовий суфікс від колізії в ту саму секунду (-1, -2, …).
var reBackupDateSuffix = regexp.MustCompile(`-(\d{8})-(\d{6})(?:-\d+)?$`)

// worldFromBackupID витягує ім'я світу з ідентифікатора бекапу
// ("world-20260823-181035" → "world", "world-pre-restore-20260823-181035"
// → "world", "my-world-20260823-181035-2" → "my-world"). Назва світу може сама
// містити дефіси, тому орієнтуємось саме на хвіст-дату, а не на перший
// дефіс. Якщо хвіст не розпізнано — повертаємо підказку-аргумент.
func worldFromBackupID(id, fallback string) string {
	loc := reBackupDateSuffix.FindStringIndex(id)
	if loc == nil || loc[0] == 0 {
		return fallback
	}
	head := id[:loc[0]]
	head = strings.TrimSuffix(head, "-pre-restore")
	head = strings.TrimSuffix(head, "-imported")
	if head != "" {
		return head
	}
	return fallback
}

// DeleteBackup видаляє бекап (zip + метафайл, якщо є).
func DeleteBackup(backupsDir, backupID string) error {
	zipPath := filepath.Join(backupsDir, backupID+".zip")
	err := os.Remove(zipPath)
	// Метафайл — необов'язковий: якщо його немає, це не помилка.
	_ = os.Remove(backupMetaPath(backupsDir, backupID))
	return err
}

// DeleteBackupForServer — видалення бекапу КОНКРЕТНОГО сервера. Існує
// окремо, бо раніше API передавало сюди глобальну теку даних лаунчера
// замість теки сервера — і «видалений» бекап лишався на диску.
func DeleteBackupForServer(serverDir, backupID string) error {
	backupID = filepath.Base(strings.TrimSpace(backupID))
	if backupID == "" || backupID == "." {
		return fmt.Errorf("не вказано бекап")
	}
	backupsDir := backupsDirFor(serverDir, "")
	zipPath := filepath.Join(backupsDir, backupID+".zip")
	if _, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("бекап %s не знайдено", backupID)
	}
	return DeleteBackup(backupsDir, backupID)
}

// RestoreBackup відновлює світ з бекапу.
//
// Раніше тут була та сама помилка, що в DeleteWorldBackup: за порожнього
// backupsDir шлях склеювався у «<id>.zip» ВІДНОСНО робочої теки лаунчера,
// тож кнопка «Відновити» завжди відповідала «бекап не знайдено», хоча
// архів лежав у <serverDir>/.shaurma/backups. Тепер тека обчислюється
// єдиною функцією backupsDirFor.
func RestoreBackup(serverDir, worldName, backupsDir, backupID string) error {
	backupID = filepath.Base(strings.TrimSpace(backupID))
	if backupID == "" || backupID == "." {
		return fmt.Errorf("не вказано бекап для відновлення")
	}
	backupsDir = backupsDirFor(serverDir, backupsDir)
	zipPath := filepath.Join(backupsDir, backupID+".zip")
	if _, err := os.Stat(zipPath); err != nil {
		return fmt.Errorf("бекап %s не знайдено", backupID)
	}

	worldName = filepath.Base(strings.TrimSpace(worldName))
	if worldName == "" || worldName == "." {
		return fmt.Errorf("не вказано світ для відновлення")
	}
	worldDir := filepath.Join(serverDir, worldName)

	// Створюємо бекап поточного стану перед відновленням.
	if _, err := os.Stat(worldDir); err == nil {
		backupName := fmt.Sprintf("%s-pre-restore-%s.zip", worldName, time.Now().Format("20060102-150405"))
		preBackup := filepath.Join(backupsDir, backupName)
		if err := zipDir(worldDir, preBackup); err != nil {
			return fmt.Errorf("бекап перед відновленням: %w", err)
		}
		writeBackupMeta(backupsDir, strings.TrimSuffix(backupName, ".zip"), 0, false)
	}

	// Видаляємо поточний світ.
	if err := os.RemoveAll(worldDir); err != nil {
		return fmt.Errorf("видалення поточного світу: %w", err)
	}

	// Розпаковуємо бекап.
	if err := unzipTo(zipPath, filepath.Dir(worldDir)); err != nil {
		return fmt.Errorf("розпакування: %w", err)
	}

	return nil
}

// ExportBackup копіює бекап у вказану директорію.
func ExportBackup(backupsDir, backupID, destPath string) error {
	src := filepath.Join(backupsDir, backupID+".zip")
	return copyFile(src, filepath.Join(destPath, backupID+".zip"))
}

// ImportBackup імпортує zip-бекап.
func ImportBackup(backupsDir, zipPath, worldName string) (string, error) {
	if strings.TrimSpace(backupsDir) == "" {
		return "", fmt.Errorf("не вказано теку бекапів")
	}
	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		return "", err
	}
	destName := fmt.Sprintf("%s-imported-%s.zip", worldName, time.Now().Format("20060102-150405"))
	dest := filepath.Join(backupsDir, destName)
	if err := copyFile(zipPath, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// ── Хелпери ────────────────────────────────────────────────────────────

func isWorldDir(dir string) bool {
	// Перевірка на level.dat
	if _, err := os.Stat(filepath.Join(dir, "level.dat")); err == nil {
		return true
	}
	// Перевірка на region/
	if _, err := os.Stat(filepath.Join(dir, "region")); err == nil {
		return true
	}
	// Перевірка на DIM* (Anvil)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "DIM") {
			return true
		}
	}
	return false
}

func dirSizeMB(path string) float64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return float64(size) / (1024 * 1024)
}

func dirModTime(path string) time.Time {
	var latest time.Time
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})
	return latest
}

// zipEntry — один файл для пакування (шлях на диску + шлях у архіві).
type zipEntry struct {
	path string
	rel  string
}

// collectZipEntries збирає ВСІ файли світу одним проходом. Потрібно, щоб
// знати загальну кількість (знаменник для відсотків прогресу) ДО початку
// пакування — інакше «50%» довелось би вигадувати.
func collectZipEntries(srcDir string) []zipEntry {
	var out []zipEntry
	base := filepath.Dir(srcDir)
	_ = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		out = append(out, zipEntry{path: path, rel: filepath.ToSlash(rel)})
		return nil
	})
	return out
}

// zipEntries пише передані файли в zip, повідомляючи про прогрес після
// кожного файлу. rel/шляхи ті самі, що писав zipDir (відносно батька
// теки світу), тож unzipTo(filepath.Dir(worldDir)) розпаковує їх як і раніше.
func zipEntries(srcDir, destZip string, entries []zipEntry, onFile func(done, total int)) error {
	f, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	total := len(entries)
	for i, e := range entries {
		fw, err := w.Create(e.rel)
		if err != nil {
			_ = w.Close()
			return err
		}
		src, err := os.Open(e.path)
		if err != nil {
			// Файл міг зникнути між скануванням і пакуванням (гра сама
			// чистить старі чанки) — пропускаємо, а не валимо весь бекап.
			if onFile != nil {
				onFile(i+1, total)
			}
			continue
		}
		_, copyErr := io.Copy(fw, src)
		src.Close()
		if copyErr != nil {
			_ = w.Close()
			return copyErr
		}
		if onFile != nil {
			onFile(i+1, total)
		}
	}
	return w.Close()
}

func zipDir(srcDir, destZip string) error {
	f, err := os.Create(destZip)
	if err != nil {
		return err
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Dir(srcDir), path)
		rel = filepath.ToSlash(rel)
		fw, err := w.Create(rel)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(fw, src)
		return err
	})
}

func unzipTo(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		outPath := filepath.Join(destDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(outPath, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(outPath), 0755)
		outFile, err := os.Create(outPath)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
