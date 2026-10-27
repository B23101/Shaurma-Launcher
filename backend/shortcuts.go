// shortcuts.go — ярлики збірок на робочому столі (аналог Modrinth App:
// «Create desktop shortcut»). ПКМ по збірці → «Створити ярлик на робочому
// столі» → на Desktop з'являється <Назва збірки>.lnk з іконкою та назвою
// збірки; подвійний клік запускає shell.exe з аргументом --launch <packID>,
// лаунчер стартує (або піднімається з трею, якщо вже запущений) і одразу
// відкриває сторінку збірки і запускає гру.
//
// Реалізація:
//   - .lnk створюється через PowerShell WScript.Shell (той самий підхід,
//     що й тости в notify.go — без жодних додаткових залежностей);
//   - іконка: PNG іконки збірки (локальна для кастомних, CDN для Shaurma)
//     конвертується в .ico простим загортанням (shortcuts.PNGToICO) і
//     кладеться в <dataDir>/shortcuts/<packID>.ico;
//   - реєстр створених ярликів — <dataDir>/shortcuts.json (packID → ім'я
//     файлу .lnk), щоб Remove/перейменування/видалення збірки знали, що
//     прибрати, навіть якщо збірку перейменували.
//
// Запуск з ярлика: див. shell/main.go (SingleInstance + парсинг
// "--launch") і handleShellEvent → подія "shortcut:launch".

package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/shortcuts"
)

// DesktopShortcut — опис створеного ярлика (для UI-повідомлень і стану).
type DesktopShortcut struct {
	PackID string `json:"packId"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	// Персональні опції запуску ярлика (перекривають налаштування збірки
	// РІВНО для запуску з цього ярлика; решта — як при звичайному запуску).
	AccountID      string `json:"accountId,omitempty"`
	AutoJoinServer string `json:"autoJoinServer,omitempty"`
}

// shortcutEntry — ЗНАЧЕННЯ в реєстрі ярликів. Раніше там був просто шлях
// до .lnk; тепер — шлях + персональні опції запуску (акаунт, автоприєднання),
// щоб діалог створення міг передзаповнити поля при редагуванні і щоб
// перестворення ярлика (наприклад після зміни назви збірки) зберігало їх.
type shortcutEntry struct {
	Path           string `json:"path"`
	AccountID      string `json:"accountId,omitempty"`
	AutoJoinServer string `json:"autoJoinServer,omitempty"`
}

// shortcutRegistry — мапа packID → shortcutEntry (закодована в JSON) або
// (старий формат) просто ім'я/шлях .lnk. Зберігається в SQLite (таблиця
// shortcuts), щоб ярлик переживав перезапуск лаунчера.
type shortcutRegistry map[string]string

// encodeShortcutEntry серіалізує запис реєстру (шлях + опції) у значення.
func encodeShortcutEntry(e shortcutEntry) string {
	b, _ := json.Marshal(e)
	return string(b)
}

// decodeShortcutEntry розбирає значення реєстру: новий формат (JSON з
// path+опціями) або старий (голий шлях/ім'я — тоді опцій немає).
func decodeShortcutEntry(v string) shortcutEntry {
	var e shortcutEntry
	if err := json.Unmarshal([]byte(v), &e); err != nil || e.Path == "" {
		return shortcutEntry{Path: v}
	}
	return e
}

// ── Шлях до робочого столу ──────────────────────────────────────────────
//
// desktopDir/startMenuDir/createLNK — винесені у shortcuts_windows.go /
// shortcuts_other.go (Частина 8, крок 1 плану: golang.org/x/sys/windows
// не можна імпортувати з файлу без build tag, інакше GOOS=linux go build
// падає на етапі компіляції ще до того, як спрацює рантайм-перевірка
// runtime.GOOS != "windows"). Реєстр ярликів і публічні App-методи
// лишаються тут спільними для всіх ОС: на Linux вони повертають
// зрозумілу помилку через ту саму error-гілку, що й раніше на macOS,
// а не зникають з reflect-диспетчера backend_invoke (інакше фронтенд
// отримав би "method not found" замість людської помилки).

// ── Реєстр ярликів ──────────────────────────────────────────────────────

// shortcutsFile повертає шлях до легасі shortcuts.json (лише для
// одноразової міграції у БД — див. migration.go). Реєстр ярликів тепер
// живе в SQLite (таблиця shortcuts).
func (a *App) shortcutsFile() string {
	return filepath.Join(a.cfg.Dir(), "shortcuts.json")
}

// loadShortcutRegistry читає реєстр ярликів зі SQLite (порожня мапа,
// якщо записів немає — це не помилка).
func (a *App) loadShortcutRegistry() shortcutRegistry {
	if st := a.cfg.Store(); st != nil {
		reg, err := st.LoadShortcuts()
		if err != nil {
			return shortcutRegistry{}
		}
		if reg == nil {
			return shortcutRegistry{}
		}
		return shortcutRegistry(reg)
	}
	// Файловий фолбек (тести/dev-без-БД).
	data, err := os.ReadFile(a.shortcutsFile())
	if err != nil {
		return shortcutRegistry{}
	}
	var reg shortcutRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return shortcutRegistry{}
	}
	if reg == nil {
		return shortcutRegistry{}
	}
	return reg
}

func (a *App) saveShortcutRegistry(reg shortcutRegistry) error {
	if st := a.cfg.Store(); st != nil {
		return st.SaveShortcuts(map[string]string(reg))
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.shortcutsFile(), data, 0644)
}

// ── Іконка збірки ───────────────────────────────────────────────────────

// packIconPNG повертає байти PNG-іконки збірки в такому порядку:
//  1. кастомна збірка з локальним файлом IconPath — читаємо його;
//  2. Shaurma-збірка — CDN-іконка через AssetDataURL (data URL);
//  3. будь-яка збірка з ПРЕСЕТ-іконкою (Tabler, напр. "ti-puzzle") +
//     кольором — рендеримо гліф шрифту в кольорі збірки (як картка в
//     лаунчері), щоб ярлик не лишався з іконкою exe за замовчуванням;
//  4. (nil, false) — іконку зовсім недоступно; ярлик з іконкою exe.
func (a *App) packIconPNG(packID string) ([]byte, bool) {
	var preset, color string

	if cp, ok := a.customPacks.Get(packID); ok {
		preset = cp.Icon
		color = cp.Color
		if cp.IconPath != "" {
			iconPath := a.packAssetURL(cp, cp.IconPath)
			// packAssetURL додає "local-file://" префікс — прибираємо для os.ReadFile
			if strings.HasPrefix(iconPath, "local-file://") {
				iconPath = iconPath[len("local-file://"):]
			}
			if b, err := os.ReadFile(iconPath); err == nil && len(b) > 0 {
				return b, true
			}
		}
	} else if a.packs != nil {
		if entry, err := a.packs.GetPackByID(packID); err == nil {
			color = entry.Color
			if entry.IconURL != "" {
				if dataURL, err := a.packs.AssetDataURL(entry.IconURL); err == nil {
					if b, ok := decodeDataURLPNG(dataURL); ok {
						return b, true
					}
				}
			}
		}
	}

	// Пресет-іконка (Tabler): рендеримо гліф у колір збірки.
	if preset != "" {
		if b, ok := shortcuts.PresetIconToPNG(preset, color, 256); ok {
			return b, true
		}
	}
	return nil, false
}

// decodeDataURLPNG витягує PNG-байти з data URL вигляду
// "data:image/png;base64,....". Повертає (nil, false) для будь-якого
// невідповідного формату (це не помилка — просто іконки немає).
func decodeDataURLPNG(dataURL string) ([]byte, bool) {
	idx := strings.Index(dataURL, ";base64,")
	if idx < 0 {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(dataURL[idx+len(";base64,"):])
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

// ensureShortcutIcon готує .ico файл іконки збірки в <dataDir>/shortcuts/.
// Повертає повний шлях до .ico або "" (іконку не вдалось отримати — ярлик
// створиться з іконкою exe за замовчуванням).
func (a *App) ensureShortcutIcon(packID string) string {
	png, ok := a.packIconPNG(packID)
	if !ok {
		return ""
	}
	ico, err := shortcuts.PNGToICO(png)
	if err != nil {
		return ""
	}
	dir := filepath.Join(a.cfg.CacheDir(), "shortcuts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ""
	}
	path := filepath.Join(dir, packID+".ico")
	if err := os.WriteFile(path, ico, 0644); err != nil {
		return ""
	}
	return path
}

// ── Створення .lnk через PowerShell WScript.Shell ──────────────────────
//
// createLNK — див. shortcuts_windows.go / shortcuts_other.go.

// ── Методи App (викликаються з фронтенду) ───────────────────────────────

// ShortcutOptions — параметри створення ярлика (аналог діалогу
// CreateShortcutDialog у Prism: назва, місце збереження, а також розові
// опції запуску — акаунт і автоприєднання до сервера). Іконка завжди
// береться зі збірки; запуск — shell.exe з аргументами
// --launch <packID> [--account <id>] [--server <host:port>].
type ShortcutOptions struct {
	// Name — назва ярлика (порожня = назва збірки).
	Name string `json:"name"`
	// Location — куди створити ярлик: "desktop" (робочий стіл),
	// "startmenu" (меню «Пуск» → Програми) або абсолютний шлях до теки
	// (довільне місце, як "Other..." у Prism).
	Location string `json:"location"`
	// AccountID — ID акаунта, з яким запускати збірку з цього ярлика
	// (порожній = активний акаунт лаунчера). Свої «нюанси» ярлика, як у
	// Prism: перекривають акаунт збірки/лаунчера РІВНО для цього запуску,
	// решта налаштувань — як при звичайному запуску.
	AccountID string `json:"accountId,omitempty"`
	// AutoJoinServer — сервер host[:port] для автоприєднання при запуску з
	// цього ярлика (порожній = налаштування автоприєднання самої збірки).
	AutoJoinServer string `json:"autoJoinServer,omitempty"`
}

// CreateDesktopShortcut створює ярлик збірки на робочому столі: назва =
// назва збірки, іконка = іконка збірки, запуск = shell.exe --launch <packID>.
// Якщо ярлик для цієї збірки вже існує — перестворюється (нові назва/
// іконка), тобто метод ідемпотентний.
func (a *App) CreateDesktopShortcut(packID string) (DesktopShortcut, error) {
	return a.CreateShortcut(packID, ShortcutOptions{Name: "", Location: "desktop"})
}

// CreateShortcut створює ярлик збірки з гнучкими налаштуваннями (назва +
// місце) — порт діалогу CreateShortcutDialog з Prism. Location: "desktop" /
// "startmenu" / абсолютний шлях до теки.
func (a *App) CreateShortcut(packID string, opts ShortcutOptions) (DesktopShortcut, error) {
	if packID == "" {
		return DesktopShortcut{}, errors.New("порожній packID")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = packID
		if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
			name = cp.Name
		} else if a.packs != nil {
			if entry, err := a.packs.GetPackByID(packID); err == nil && entry.Name != "" {
				name = entry.Name
			}
		}
	}

	targetDir, err := shortcutTargetDir(opts.Location)
	if err != nil {
		return DesktopShortcut{}, err
	}

	// Іконка .ico (якщо збірка її має).
	icoPath := a.ensureShortcutIcon(packID)
	iconLocation := ""
	if icoPath != "" {
		iconLocation = icoPath + ",0"
	}

	// Ім'я файлу: назва ярлика (санітизована). Дублікатів не буде — якщо
	// користувач створює ярлик для двох збірок з однаковими назвами,
	// старіший ярлик перезапишеться (краще, ніж таємний суфікс у назві).
	// Якщо назва після санітизації порожня (складалась лише з розділювачів/
	// заборонених символів) — беремо packID, а не абстрактне "pack":
	// інакше .lnk у меню «Пуск» називався б просто ".lnk" (відомий баг).
	lnkName := sanitizeFileName(name)
	if lnkName == "pack" {
		if pid := sanitizeFileName(packID); pid != "pack" && pid != "" {
			lnkName = pid
		}
	}
	lnkName += ".lnk"
	lnkPath := filepath.Join(targetDir, lnkName)

	// Реєстр: прибираємо ПОПЕРЕДНІЙ ярлик цієї збірки (якщо був під іншою
	// назвою) І чужий ярлик з такою самою назвою (інша збірка-дублікат).
	// Це не «рухає» ярлик: юзер сам викликав «Створити ярлик» — очікувано,
	// що старий .lnk цієї збірки не лишиться висіти дублем.
	reg := a.loadShortcutRegistry()
	for pid, oldPath := range reg {
		if filepath.Base(oldPath) == lnkName && pid != packID {
			_ = os.Remove(oldPath)
			delete(reg, pid)
		}
		if pid == packID && oldPath != lnkPath {
			_ = os.Remove(oldPath)
			delete(reg, pid)
		}
	}

	exe := execPath()
	args := a.shortcutLaunchArgs(packID, opts)
	desc := fmt.Sprintf("%s — Shaurma Launcher", name)

	if err := createLNK(lnkPath, exe, args, filepath.Dir(exe), iconLocation, desc); err != nil {
		return DesktopShortcut{}, err
	}

	reg[packID] = encodeShortcutEntry(shortcutEntry{
		Path: lnkPath, AccountID: opts.AccountID, AutoJoinServer: opts.AutoJoinServer,
	})
	_ = a.saveShortcutRegistry(reg)

	return DesktopShortcut{
		PackID: packID, Name: name, Path: lnkPath, Exists: true,
		AccountID: opts.AccountID, AutoJoinServer: opts.AutoJoinServer,
	}, nil
}

// shortcutLaunchArgs будує CLI-аргументи для ярлика збірки:
//
//	--launch <packID> [--account <id>] [--server <host:port>]
//
// --account / --server — персональні опції запуску ЯРЛИКА (як у Prism):
// перекривають акаунт збірки/лаунчера та автоприєднання збірки РІВНО для
// запуску з цього ярлика; решта параметрів (Java, RAM, моди тощо)
// лишається як при звичайному запуску. Shell розбирає ці аргументи і
// передає у backend (shortcut:launch), а той — у фронтенд для запуску.
func (a *App) shortcutLaunchArgs(packID string, opts ShortcutOptions) string {
	args := "--launch " + packID
	if opts.AccountID != "" {
		args += " --account " + opts.AccountID
	}
	if opts.AutoJoinServer != "" {
		args += " --server " + opts.AutoJoinServer
	}
	return args
}

// shortcutTargetDir визначає теку призначення ярлика за Location.
func shortcutTargetDir(location string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(location)) {
	case "", "desktop":
		return desktopDir()
	case "startmenu":
		return startMenuDir()
	default:
		// Довільна тека ("Other...") — створюємо, якщо її немає.
		dir := strings.TrimSpace(location)
		if dir == "" {
			return "", errors.New("порожня тека ярлика")
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("тека ярлика недоступна: %w", err)
		}
		return dir, nil
	}
}

// startMenuDir повертає теку меню «Пуск» → Програми поточного користувача
// (FOLDERID_Programs — %APPDATA%\Microsoft\Windows\Start Menu\Programs).
// Реалізація — shortcuts_windows.go / shortcuts_other.go.

// resolveShortcutPath — registry зберігає ПОВНИЙ шлях до .lnk (новий
// формат — JSON shortcutEntry; старий — голий шлях або ім'я файлу на
// робочому столі). Приводимо всі формати до абсолютного шляху.
func (a *App) resolveShortcutPath(regVal string) string {
	return a.resolveShortcutEntry(regVal).Path
}

// resolveShortcutEntry — те саме, що resolveShortcutPath, але повертає
// ПОВНИЙ запис (шлях + персональні опції запуску) для UI-редагування.
func (a *App) resolveShortcutEntry(regVal string) shortcutEntry {
	if regVal == "" {
		return shortcutEntry{}
	}
	e := decodeShortcutEntry(regVal)
	path := e.Path
	// Старий формат: ім'я .lnk на робочому столі (без розділювача).
	if !strings.ContainsAny(path, "\\/") && path != "" {
		desktop, err := desktopDir()
		if err == nil {
			path = filepath.Join(desktop, path)
		}
	}
	e.Path = path
	return e
}

// UpdateDesktopShortcut оновлює ІСНУЮЧИЙ ярлик збірки при зміні її назви/
// іконки (викликається з UpdateCustomPack після збереження форми).
//
// ГЛАВНЕ: ярлик НЕ переміщується на робочому столі. Windows зберігає
// позицію іконки за об'єктом файлу, а не за ім'ям — тому перейменування
// .lnk на місці (os.Rename — те саме, що F2 вручну) лишає ярлик там, де
// користувач його розмістив. Створення НОВОГО файлу під новою назвою
// (стара реалізація через CreateDesktopShortcut) давала б другий ярлик
// на іншому місці — це і був баг.
//
// Якщо ярлика для цієї збірки немає (юзер його видалив або ніколи не
// створював) — no-op: зміна назви збірки НЕ повинна воскрешати ярлик.
func (a *App) UpdateDesktopShortcut(packID string) error {
	reg := a.loadShortcutRegistry()
	oldVal, ok := reg[packID]
	if !ok {
		return nil
	}
	oldEntry := a.resolveShortcutEntry(oldVal)
	oldPath := oldEntry.Path
	oldName := filepath.Base(oldPath)

	// Юзер видалив ярлик вручну (файл зник, але запис у реєстрі лишився) —
	// прибираємо запис, нічого не створюємо.
	if _, err := os.Stat(oldPath); err != nil {
		delete(reg, packID)
		_ = a.saveShortcutRegistry(reg)
		return nil
	}

	// Актуальна назва та іконка збірки (після збереження форми).
	name := packID
	if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
		name = cp.Name
	} else if a.packs != nil {
		if entry, err := a.packs.GetPackByID(packID); err == nil && entry.Name != "" {
			name = entry.Name
		}
	}
	newName := sanitizeFileName(name)
	if newName == "pack" {
		if pid := sanitizeFileName(packID); pid != "pack" && pid != "" {
			newName = pid
		}
	}
	newName += ".lnk"
	// Перейменовуємо НА МІСЦІ (у тій самій теці, де лежить ярлик — робочий
	// стіл, меню «Пуск» чи довільна тека). Windows зберігає позицію іконки
	// за об'єктом файлу, а не за ім'ям — rename лишає ярлик там, де
	// користувач його розмістив.
	newPath := filepath.Join(filepath.Dir(oldPath), newName)

	// Якщо назва змінилась — перейменовуємо файл НА МІСЦІ. Якщо ціль
	// зайнята чужим ярликом з реєстру (інша збірка з такою самою назвою
	// в тій самій теці) — прибираємо його, щоб не було колізії.
	if newName != oldName && newPath != oldPath {
		for pid, val := range reg {
			other := a.resolveShortcutPath(val)
			if pid != packID && filepath.Clean(other) == filepath.Clean(newPath) {
				_ = os.Remove(other)
				delete(reg, pid)
			}
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			// Файл перейменувати не вдалось (напр. заблокований) —
			// лишаємо старий ярлик на місці і просто оновлюємо його вміст.
			newPath = oldPath
			newName = oldName
		}
	}

	// Оновлюємо вміст .lnk (іконка, опис) за тим самим файлом — назва
	// ярлика = ім'я файлу, воно вже змінено вище.
	icoPath := a.ensureShortcutIcon(packID)
	iconLocation := ""
	if icoPath != "" {
		iconLocation = icoPath + ",0"
	}
	exe := execPath()
	// Персональні опції запуску ярлика зберігаємо ПРИ ПЕРЕСТВОРЕННІ:
	// акаунт/сервер, обрані у діалозі, не повинні губитись при зміні
	// назви/іконки збірки (UpdateDesktopShortcut викликається з форми).
	args := a.shortcutLaunchArgs(packID, ShortcutOptions{
		AccountID: oldEntry.AccountID, AutoJoinServer: oldEntry.AutoJoinServer,
	})
	desc := fmt.Sprintf("%s — Shaurma Launcher", name)
	if err := createLNK(newPath, exe, args, filepath.Dir(exe), iconLocation, desc); err != nil {
		return err
	}

	reg[packID] = encodeShortcutEntry(shortcutEntry{
		Path: newPath, AccountID: oldEntry.AccountID, AutoJoinServer: oldEntry.AutoJoinServer,
	})
	_ = a.saveShortcutRegistry(reg)
	return nil
}

// RenameShortcut переносить ярлик збірки з oldID на newID при зміні ID
// кастомної збірки (ChangeCustomPackID): .lnk перестворюється НА МІСЦІ з
// новими аргументами (--launch <newID>), а запис у реєстрі переїжджає з
// ключа oldID на newID, зберігаючи персональні опції запуску (акаунт/
// автоприєднання). Якщо ярлика немає — no-op.
func (a *App) RenameShortcut(oldID, newID string) error {
	reg := a.loadShortcutRegistry()
	oldVal, ok := reg[oldID]
	if !ok {
		return nil
	}
	oldEntry := a.resolveShortcutEntry(oldVal)
	oldPath := oldEntry.Path

	// Юзер видалив ярлик вручну (файл зник, запис лишився) — прибираємо
	// запис, нічого не створюємо.
	if _, err := os.Stat(oldPath); err != nil {
		delete(reg, oldID)
		_ = a.saveShortcutRegistry(reg)
		return nil
	}

	// Перестворюємо .lnk на тому самому місці з новим --launch <newID> і
	// збереженими персональними опціями ярлика.
	icoPath := a.ensureShortcutIcon(newID)
	iconLocation := ""
	if icoPath != "" {
		iconLocation = icoPath + ",0"
	}
	exe := execPath()
	args := a.shortcutLaunchArgs(newID, ShortcutOptions{
		AccountID: oldEntry.AccountID, AutoJoinServer: oldEntry.AutoJoinServer,
	})
	name := newID
	if cp, ok := a.customPacks.Get(newID); ok && cp.Name != "" {
		name = cp.Name
	}
	desc := fmt.Sprintf("%s — Shaurma Launcher", name)
	if err := createLNK(oldPath, exe, args, filepath.Dir(exe), iconLocation, desc); err != nil {
		return err
	}

	delete(reg, oldID)
	reg[newID] = encodeShortcutEntry(shortcutEntry{
		Path: oldPath, AccountID: oldEntry.AccountID, AutoJoinServer: oldEntry.AutoJoinServer,
	})
	return a.saveShortcutRegistry(reg)
}

// RemoveDesktopShortcut видаляє ярлик збірки (робочий стіл, меню «Пуск»
// чи довільна тека — див. реєстр). Видалення ярлика, якого немає, — не
// помилка.
func (a *App) RemoveDesktopShortcut(packID string) error {
	reg := a.loadShortcutRegistry()
	lnkVal, ok := reg[packID]
	if !ok {
		return nil
	}
	lnkPath := a.resolveShortcutPath(lnkVal)
	_ = os.Remove(lnkPath)

	delete(reg, packID)
	_ = a.saveShortcutRegistry(reg)

	// Іконку .ico також прибираємо (вона генерується, її завжди можна
	// відновити повторним CreateShortcut).
	_ = os.Remove(filepath.Join(a.cfg.CacheDir(), "shortcuts", packID+".ico"))
	return nil
}

// HasDesktopShortcut повідомляє, чи існує ярлик цієї збірки (для UI:
// показувати «Створити ярлик» чи «Видалити ярлик»).
func (a *App) HasDesktopShortcut(packID string) bool {
	reg := a.loadShortcutRegistry()
	lnkVal, ok := reg[packID]
	if !ok {
		return false
	}
	_, err := os.Stat(a.resolveShortcutPath(lnkVal))
	return err == nil
}

// GetDesktopShortcut повертає стан ярлика збірки (для синхронізації UI
// після перейменування збірки чи інших зовнішніх змін).
func (a *App) GetDesktopShortcut(packID string) DesktopShortcut {
	name := packID
	if cp, ok := a.customPacks.Get(packID); ok && cp.Name != "" {
		name = cp.Name
	} else if a.packs != nil {
		if entry, err := a.packs.GetPackByID(packID); err == nil && entry.Name != "" {
			name = entry.Name
		}
	}
	reg := a.loadShortcutRegistry()
	lnkVal, ok := reg[packID]
	path := ""
	exists := false
	var accID, joinServer string
	if ok {
		e := a.resolveShortcutEntry(lnkVal)
		path = e.Path
		accID = e.AccountID
		joinServer = e.AutoJoinServer
		_, err := os.Stat(path)
		exists = err == nil
	}
	return DesktopShortcut{
		PackID: packID, Name: name, Path: path, Exists: exists,
		AccountID: accID, AutoJoinServer: joinServer,
	}
}

// GetAllShortcuts повертає список УСІХ створених ярликів (для менеджера
// ярликів у налаштуваннях): кожен з назвою збірки, шляхом .lnk, прапорцем
// існування і персональними опціями запуску (акаунт/автоприєднання).
// Упорядковані за назвою збірки (стабільно для UI).
func (a *App) GetAllShortcuts() []DesktopShortcut {
	reg := a.loadShortcutRegistry()
	out := make([]DesktopShortcut, 0, len(reg))
	for pid := range reg {
		sc := a.GetDesktopShortcut(pid)
		if sc.Path == "" {
			continue
		}
		out = append(out, sc)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// ── Автозапуск збірки з ярлика ──────────────────────────────────────────

// PendingShortcutLaunch — дані запуску з ярлика: packID + персональні
// опції ярлика (акаунт, автоприєднання). Зберігається на бекенді, бо
// подія shortcut:launch від shell може прийти ДО того, як фронтенд
// підписався на події (старт лаунчера); фронтенд забирає значення через
// TakePendingShortcutLaunch на маунті.
type PendingShortcutLaunch struct {
	PackID         string `json:"packId"`
	AccountID      string `json:"accountId,omitempty"`
	AutoJoinServer string `json:"autoJoinServer,omitempty"`
}

var (
	pendingShortcutLaunch   PendingShortcutLaunch
	pendingShortcutLaunchMu sync.Mutex
)

// setPendingShortcutLaunch запам'ятовує дані запуску з ярлика (packID +
// опції). Останній виклик перемагає (повторні кліки по ярликах).
func setPendingShortcutLaunch(p PendingShortcutLaunch) {
	pendingShortcutLaunchMu.Lock()
	pendingShortcutLaunch = p
	pendingShortcutLaunchMu.Unlock()
}

// TakePendingShortcutLaunch повертає і очищає дані запуску, передані через
// "--launch" (packID + опції ярлика). Фронтенд викликає це один раз на
// маунті — якщо лаунчер запущено з ярлика, збірка одразу відкриється і
// запуститься (з опціями ярлика, якщо вони задані).
func (a *App) TakePendingShortcutLaunch() PendingShortcutLaunch {
	pendingShortcutLaunchMu.Lock()
	defer pendingShortcutLaunchMu.Unlock()
	p := pendingShortcutLaunch
	pendingShortcutLaunch = PendingShortcutLaunch{}
	return p
}
