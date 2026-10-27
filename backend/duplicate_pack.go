package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/export"
)

// ── Дублювання збірок (не-Shaurma, кастомні створюються як нові записи) ──
// Модалка «Дублювати» на картці/контекстному меню збірки: користувач обирає
// в дереві, які папки/файли скопіювати (той самий UI, що в експорті), чи
// дублювати іконку або задати свою (пресет + колір), назву/ID (ID має бути
// вільним) і чи копіювати налаштування збірки (per-instance конфіг).
// Результат — НОВА кастомна збірка у custompack.Store, повністю незалежна
// від джерела (для Shaurma-джерела «Оновити з джерела» не чіпає копію).

// DuplicatePackIDCheck — результат перевірки ID перед дублюванням.
// Exists=true означає, що ID вже зайнятий (збірка Shaurma АБО кастомна) —
// тоді фронтенд підказує Suggested.
type DuplicatePackIDCheck struct {
	ID        string `json:"id"`
	Exists    bool   `json:"exists"`
	Suggested string `json:"suggested"`
}

// packIDExists — чи ID збірки вже зайнятий БУДЬ-ЯКОЮ збіркою (Shaurma з
// каталогу або локальною кастомною). Перевіряємо обидва джерела: теки
// збірок лежать в одному <InstanceDir>/<id>, тож колізія зі Shaurma-паком
// затерла б файли чужої збірки.
func (a *App) packIDExists(id string) bool {
	if id == "" {
		return true
	}
	if _, ok := a.customPacks.Get(id); ok {
		return true
	}
	if a.packs != nil {
		if index, err := a.packs.GetPackIndex(); err == nil {
			for _, e := range index {
				if e.ID == id {
					return true
				}
			}
		}
	}
	return false
}

// CheckDuplicatePackID перевіряє, чи запропонований користувачем ID уже
// зайнятий, і підказує перший вільний варіант (id, id-2, id-3, …).
func (a *App) CheckDuplicatePackID(id string) DuplicatePackIDCheck {
	slug := custompack.SlugifyName(id)
	if slug == "" {
		slug = "pack"
	}
	suggested := slug
	for i := 2; a.packIDExists(suggested); i++ {
		suggested = fmt.Sprintf("%s-%d", slug, i)
	}
	return DuplicatePackIDCheck{
		ID:        slug,
		Exists:    a.packIDExists(slug),
		Suggested: suggested,
	}
}

// GetDuplicatePackTree повертає плоский список кандидатів для дерева
// вибору у модалці дублювання (той самий формат, що ListExportCandidates —
// папки першими, з прапором defaultSelected для пре-вибору).
func (a *App) GetDuplicatePackTree(packID string) ([]export.ExportCandidate, error) {
	gameDir := a.packRootDir(packID)
	if gameDir == "" {
		return nil, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	if _, err := os.Stat(gameDir); err != nil {
		return nil, fmt.Errorf("теку збірки ще не створено — спершу завантажте збірку")
	}
	return export.ScanCandidates(gameDir)
}

// DuplicatePackRequest — параметри дублювання з фронтенду.
type DuplicatePackRequest struct {
	SourcePackID string `json:"sourcePackId"`
	// NewID — бажаний ID нової збірки (slug). Якщо порожній/зайнятий —
	// бекенд сам згенерує вільний. Має бути вільним (перевірка нижче).
	NewID string `json:"newId"`
	// NewName — назва копії. Порожня → "<Назва джерела> (2)" / "(3)"…
	// (той самий дефолт, що у Modrinth App для копій збірок).
	NewName string `json:"newName"`
	// Description — опис копії (з джерела або введений користувачем).
	Description string `json:"description"`

	// ── Іконка/колір ──
	// CopyIcon — скопіювати іконку джерела (PNG якщо є, інакше пресет +
	// колір). false = користувач налаштовує сам через Icon (пресет) і Color.
	CopyIcon bool `json:"copyIcon"`
	// IconSrcPath — локальний файл іконки, обраний користувачем у модалці
	// (діалог вибору зображення). Має пріоритет над CopyIcon.
	IconSrcPath string `json:"iconSrcPath"`
	// Icon — пресет-іконка (Tabler, напр. "ti-puzzle").
	Icon string `json:"icon"`
	// UseCustomColor — true = використати Color нижче; false = успадкувати
	// колір джерела (або колір пресета/дефолт).
	UseCustomColor bool   `json:"useCustomColor"`
	Color          string `json:"color"`

	// CopySettings — скопіювати налаштування збірки: per-instance конфіг
	// (RAM, JVM-аргументи, вікно, команди, автоприєднання, скін збірки,
	// закріплені сервери) + RAM/Java з опису кастомного джерела.
	CopySettings bool `json:"copySettings"`

	// Included/Excluded — відносні шляхи (Unix '/') з UI-дерева, той самий
	// контракт, що в експорті. Порожній Included = копіювати ВСЕ.
	Included []string `json:"included"`
	Excluded []string `json:"excluded"`
}

// DuplicatePackResult — результат дублювання: новий запис збірки + кількість
// скопійованих файлів.
type DuplicatePackResult struct {
	Pack        custompack.Pack `json:"pack"`
	FilesCopied int             `json:"filesCopied"`
	TotalBytes  int64           `json:"totalBytes"`
}

// DuplicateProgress — одна подія прогресу дублювання (emit "duplicate:progress").
type DuplicateProgress struct {
	Stage    string `json:"stage"` // copy | done
	Current  int    `json:"current"`
	Total    int    `json:"total"`
	FileName string `json:"fileName,omitempty"`
}

// DuplicatePack створює копію збірки: резервує вільний ID, створює теку
// нової збірки, копіює обрані файли (той самий вибір, що в експорті),
// за бажанням — іконку/колір та налаштування, і реєструє нову кастомну
// збірку в Store. Працює для БУДЬ-ЯКОГО джерела (Shaurma або кастомна);
// результат завжди кастомна збірка, незалежна від джерела.
func (a *App) DuplicatePack(req DuplicatePackRequest) (DuplicatePackResult, error) {
	if req.SourcePackID == "" {
		return DuplicatePackResult{}, fmt.Errorf("не вказано збірку-джерело")
	}
	src, err := a.resolveLaunchBuild(req.SourcePackID)
	if err != nil {
		return DuplicatePackResult{}, err
	}
	// Офіційні Shaurma-збірки НЕ дублюються: вони керуються лаунчером і
	// синкуються з каталогу (копія все одно перезапишеться при оновленні),
	// тож дублювання для них — плутанина, а не користь (аналогічно експорту,
	// errExportForbidden). Працює дублювання лише для кастомних збірок.
	if src.IsShaurma {
		return DuplicatePackResult{}, fmt.Errorf("дублювання офіційних Shaurma-збірок заборонено")
	}
	gameDir := a.packRootDir(req.SourcePackID)
	if gameDir == "" {
		return DuplicatePackResult{}, fmt.Errorf("теку збірки не знайдено: %s", req.SourcePackID)
	}
	if _, err := os.Stat(gameDir); err != nil {
		return DuplicatePackResult{}, fmt.Errorf("збірку ще не завантажено — дублювати можна лише встановлену збірку")
	}

	// ── Назва та ID ──
	// Дефолт назви: "<Назва джерела> (2)" / "(3)"… через UniqueIDAndName —
	// той самий підхід, що при створенні модпаку, але враховує ТІЛЬКИ
	// кастомні збірки (колізію зі Shaurma-паками закриває packIDExists).
	name := strings.TrimSpace(req.NewName)
	var newID string
	if name == "" {
		var suggestedName string
		newID, suggestedName = a.customPacks.UniqueIDAndName(src.Name)
		name = suggestedName
	} else {
		// Назва задана — ID теж має бути звідси (slug назви), але вільним.
		newID = custompack.SlugifyName(name)
		if newID == "" {
			newID = "pack"
		}
		if a.packIDExists(newID) {
			// Запропонований ID зайнятий — не падаємо, а підбираємо вільний
			// (той самий суфікс-підхід (2)/(3)), щоб копія гарантовано
			// створилась; фронтенд показує фінальний ID у результаті.
			for i := 2; a.packIDExists(newID); i++ {
				newID = fmt.Sprintf("%s-%d", newID, i)
			}
		}
	}
	// Явний запит ID з фронтенду (користувач сам ввів) — він теж має бути
	// вільним; якщо зайнятий — помилка, бо юзер свідомо обрав цей ID.
	if strings.TrimSpace(req.NewID) != "" {
		want := custompack.SlugifyName(req.NewID)
		if want == "" {
			return DuplicatePackResult{}, fmt.Errorf("ID збірки не може бути порожнім")
		}
		if a.packIDExists(want) {
			return DuplicatePackResult{}, fmt.Errorf("ID %s вже зайнятий — обери інший", want)
		}
		newID = want
	}

	// ── Іконка/колір ──
	srcPack, isCustom := a.customPacks.Get(req.SourcePackID)
	icon := req.Icon
	color := req.Color
	if !req.UseCustomColor && color == "" {
		if isCustom {
			color = srcPack.Color
		}
	}
	if req.CopyIcon {
		if isCustom {
			if srcPack.IconPath != "" {
				// Скопіюємо сам PNG джерела.
				req.IconSrcPath = srcPack.IconPath
			}
			if icon == "" {
				icon = srcPack.Icon
			}
			if color == "" {
				color = srcPack.Color
			}
		}
	}

	// ── Створення теки нової збірки ──
	newPackDir := a.packDir(newID, "")
	if err := os.MkdirAll(filepath.Join(newPackDir, ".minecraft"), 0755); err != nil {
		return DuplicatePackResult{}, fmt.Errorf("не вдалося створити теку збірки: %w", err)
	}

	// ── Копіювання файлів (той самий вибір, що в експорті) ──
	filesCopied, err := export.CopySelected(gameDir, filepath.Join(newPackDir, ".minecraft"),
		req.Included, req.Excluded,
		func(p export.Progress) {
			a.emit("duplicate:progress", DuplicateProgress{
				Stage:    "copy",
				Current:  p.Current,
				Total:    p.Total,
				FileName: p.FileName,
			})
		})
	if err != nil {
		// Часткову копію прибираємо, щоб не лишати «напів-збірку» без
		// запису в Store (її неможливо було б відкрити/видалити карткою).
		_ = os.RemoveAll(newPackDir)
		return DuplicatePackResult{}, fmt.Errorf("копіювання файлів: %w", err)
	}

	// ── Іконка у теці копії ──
	var iconPath string
	if req.IconSrcPath != "" {
		if dst, err := copyDuplicateAssetInto(newPackDir, "icon", req.IconSrcPath); err == nil {
			iconPath = dst
		}
	}

	// ── Опис: заповнюємо з кастомного джерела, інакше — введений юзером ──
	description := strings.TrimSpace(req.Description)
	if isCustom && description == "" {
		description = srcPack.Description
	}

	// ── Опис нової збірки ──
	pack := custompack.Pack{
		ID:            newID,
		Name:          name,
		Description:   description,
		Icon:          icon,
		IconPath:      iconPath,
		Color:         color,
		MCVersion:     src.MCVersion,
		Loader:        src.Loader,
		LoaderVersion: src.LoaderVersion,
		// RAM/Java успадковуємо з кастомного джерела (для Shaurma — глобальні).
		UseCustomRAM:    isCustom && srcPack.UseCustomRAM,
		MinRAMMB:        srcPack.MinRAMMB,
		MaxRAMMB:        srcPack.MaxRAMMB,
		UseSeparateJava: isCustom && srcPack.UseSeparateJava,
		JavaPath:        srcPack.JavaPath,
		// Копія завжди незалежна: Source="manual" і без джерельних ID, щоб
		// «Оновити з джерела»/архів оригіналу не чіпали копію.
		Source: "manual",
	}
	if err := a.customPacks.Save(pack); err != nil {
		_ = os.RemoveAll(newPackDir)
		return DuplicatePackResult{}, fmt.Errorf("не вдалося зберегти копію збірки: %w", err)
	}

	// ── Налаштування збірки (per-instance конфіг) ──
	if req.CopySettings {
		cfg := a.cfg.GetInstanceConfig(req.SourcePackID)
		cfg.ID = newID
		if err := a.cfg.SaveInstanceConfig(cfg); err != nil {
			// Помилка конфіга не критична — збірка все одно працює з
			// глобальними налаштуваннями (як і при CreateCustomPack).
			_ = err
		}
	} else if pack.UseCustomRAM && pack.MaxRAMMB > 0 {
		// Навіть без повного копіювання конфіга — RAM з опису джерела сіємо
		// в конфіг копії (той самий патерн, що CreateCustomPack), інакше
		// сторінка збірки показувала б «глобальні» попри UseCustomRAM.
		cfg := a.cfg.GetInstanceConfig(newID)
		cfg.MaxRAMOverride = pack.MaxRAMMB
		cfg.MinRAMOverride = pack.MinRAMMB
		_ = a.cfg.SaveInstanceConfig(cfg)
	}

	// Кастомна збірка створена з повною текою файлів — одразу «встановлена».
	a.buildsReg.SetInstalled(newID, builds.KindCustom, "")
	a.emit("duplicate:progress", DuplicateProgress{Stage: "done", Total: filesCopied})
	a.emit("builds:changed", newID)
	return DuplicatePackResult{Pack: pack, FilesCopied: filesCopied}, nil
}

// copyDuplicateAssetInto — копіює файл іконки в теку копії під фіксованим
// іменем icon.<ext> (той самий контракт, що copyAssetInto у custompack,
// але без зовнішнього залежності від неекспортованої функції).
func copyDuplicateAssetInto(packDir, baseName, srcPath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(srcPath))
	if ext == "" {
		ext = ".png"
	}
	dst := filepath.Join(packDir, baseName+ext)
	in, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return "", err
	}
	// Повертаємо ВІДНОСНИЙ шлях (лише ім'я файлу).
	return baseName + ext, nil
}

// ── Персистентні параметри дублювання (DuplicatePackModal), окремо для
// кожної збірки-джерела — той самий принцип, що GetExportSettings/
// SaveExportSettings в export_pack.go, окрема таблиця (duplicate_settings,
// див. коментар біля міграції версії 8). Раніше DuplicatePackModal взагалі
// не зберігала вибір між відкриттями — кожен раз дефолтний набір файлів,
// незалежно від того, що користувач обирав минулого разу для ЦІЄЇ
// конкретної збірки.

// GetDuplicateSettings повертає збережений JSON параметрів останнього
// дублювання для збірки-джерела packID, або "" якщо нічого не збережено.
func (a *App) GetDuplicateSettings(packID string) string {
	if packID == "" {
		return ""
	}
	st := a.cfg.Store()
	if st == nil {
		return ""
	}
	data, ok, err := st.LoadDuplicateSettings(packID)
	if err != nil || !ok {
		return ""
	}
	return data
}

// SaveDuplicateSettings зберігає JSON параметрів дублювання для збірки-
// джерела. Мовчки нічого не робить, якщо БД недоступна.
func (a *App) SaveDuplicateSettings(packID, data string) error {
	if packID == "" {
		return nil
	}
	st := a.cfg.Store()
	if st == nil {
		return nil
	}
	return st.SaveDuplicateSettings(packID, data)
}
