package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/mcoptions"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/mods"
)

// packDirForPack повертає ТЕКУ збірки (БЕЗ .minecraft).
// Саме її очікує API internal/mcoptions: OptionsFilePath/ParseFile/WriteFile
// самі додають підтеку ".minecraft" до переданої теки збірки.
//
// ВАЖЛИВО: раніше сюди передавали теку .minecraft (gameDir), і mcoptions
// шукав options.txt у <InstanceDir>/<packID>/.minecraft/.minecraft/ — у
// НЕПРАВИЛЬНОМУ місці. Розділ «Гра» на сторінці збірки «не знаходив»
// options.txt, а дефолти «За замовчуванням» писались у порожню вкладену
// теку, поки гра працювала зі справжнім файлом у .minecraft/.
func (a *App) packDirForPack(packID string) string {
	return a.packDir(packID, "")
}

// ── Налаштування гри «за замовчуванням» (вкладка налаштувань лаунчера) ──

// GetMinecraftDefaults повертає збережені дефолтні налаштування гри.
func (a *App) GetMinecraftDefaults() model.MinecraftDefaults {
	return a.cfg.GetMinecraftDefaults()
}

// SaveMinecraftDefaults зберігає дефолтні налаштування гри.
func (a *App) SaveMinecraftDefaults(d model.MinecraftDefaults) error {
	return a.cfg.SaveMinecraftDefaults(d)
}

// ResetMinecraftDefaults скидає значення до заводських (Enabled лишається
// незмінним — щоб користувач не втратив вимикач під час скидання).
func (a *App) ResetMinecraftDefaults() error {
	d := model.DefaultMinecraftDefaults()
	d.Enabled = a.cfg.GetMinecraftDefaults().Enabled
	return a.cfg.SaveMinecraftDefaults(d)
}

// ImportMinecraftDefaultsFromPack імпортує «видимі» опції options.txt
// вказаної збірки у дефолтні налаштування лаунчера (кнопка «Імпортувати
// з збірки»). Enabled зберігається як був.
func (a *App) ImportMinecraftDefaultsFromPack(packID string) (model.MinecraftDefaults, error) {
	cur := a.cfg.GetMinecraftDefaults()
	d, err := mcoptions.ImportFromPack(a.packDirForPack(packID))
	if err != nil {
		return model.MinecraftDefaults{}, err
	}
	d.Enabled = cur.Enabled
	return d, nil
}

// ── Розділ «Гра» на сторінці збірки (пряме читання/редагування options.txt) ──

// GetPackOptions повертає знімок options.txt збірки: чи файл існує, які
// «видимі» опції в ньому стоять, і абсолютний шлях до файла.
func (a *App) GetPackOptions(packID string) (model.PackOptionsView, error) {
	packDir := a.packDirForPack(packID)
	view := model.PackOptionsView{
		Options: map[string]string{},
		Path:    mcoptions.OptionsFilePath(packDir),
	}
	if _, err := os.Stat(view.Path); os.IsNotExist(err) {
		return view, nil // файла немає — це не помилка, просто показуємо про це
	} else if err != nil {
		return model.PackOptionsView{}, err
	}
	opts, err := mcoptions.GetVisibleOptions(packDir)
	if err != nil {
		return model.PackOptionsView{}, err
	}
	view.Exists = true
	view.Options = opts
	return view, nil
}

// SavePackOptions перезаписує «видимі» опції options.txt збірки з розділу
// «Гра». Решту файла не чіпає; якщо файла немає — створює. Пряме
// редагування = користувач бере контроль над файлом (маркер прибирається).
func (a *App) SavePackOptions(packID string, options map[string]string) error {
	if options == nil {
		options = map[string]string{}
	}
	return mcoptions.SetVisibleOptions(a.packDirForPack(packID), options)
}

// CopyPackOptionsFromPack копіює «видимі» опції options.txt з іншої збірки
// в цю (не чіпаючи інші опції цільового файла).
func (a *App) CopyPackOptionsFromPack(srcPackID, dstPackID string) error {
	return mcoptions.CopyVisible(a.packDirForPack(srcPackID), a.packDirForPack(dstPackID))
}

// GetKeyBindLabels повертає СПРАВЖНІ (з lang-файлів модів, а не вгадані)
// людяні підписи для біндів клавіш модів, знайдених у options.txt збірки.
// Мова гри визначається з самого options.txt (ключ "lang", як пише сама
// гра); якщо не задано — англійська. Ключ модида без встановленого мода
// в mods/ (файл видалили) у результат не потрапляє.
func (a *App) GetKeyBindLabels(packID string, modBindKeys []string) map[string]mods.KeyBindLabel {
	packDir := a.packDirForPack(packID)
	lang := "en_us"
	if opts, err := mcoptions.ParseFile(mcoptions.OptionsFilePath(packDir)); err == nil {
		if v := strings.TrimSpace(opts["lang"]); v != "" {
			lang = v
		}
	}
	return a.modMgr.KeyBindLabels(a.instanceModsDir(packID), modBindKeys, lang)
}

// ApplyMinecraftDefaultsToPack застосовує дефолти до options.txt конкретної
// збірки прямо зараз (кнопка на сторінці збірки).
func (a *App) ApplyMinecraftDefaultsToPack(packID string) error {
	d := a.cfg.GetMinecraftDefaults()
	if !d.Enabled {
		return fmt.Errorf("налаштування «За замовчуванням» вимкнено в налаштуваннях лаунчера")
	}
	return mcoptions.ApplyNow(a.packDirForPack(packID), d)
}

// ── Внутрішні виклики зі сценаріїв (качка/імпорт/запуск) ───────────────

// applyMinecraftDefaultsNow тихо застосовує дефолти після скачування збірки
// Shaurma чи імпорту mrpack/zip. Якщо функцію вимкнено — нічого не робить.
//
// ЄДИНЕ джерело правди «дефолти вже застосовувались цій збірці» —
// model.InstanceConfig.MinecraftDefaultsApplied у instance.json ЦІЄЇ
// збірки (internal/config.Manager.GetInstanceConfig/SaveInstanceConfig,
// internal/store — фізично <InstanceDir>/<packID>/instance.json, у ТІЙ
// самій теці, де лежить сама збірка — shaurma_packs/<id>/ чи
// installations/<id>/, залежно від типу, resolveInstanceDir підбирає
// правильну автоматично).
//
// РАНІШЕ (виправлено): єдиним записом «дефолти вже застосовувались» був
// рядок shaurma_pack:true ВСЕРЕДИНІ САМОГО options.txt (internal/
// mcoptions.MarkerKey) — тобто службовий стан лаунчера жив у файлі
// гравця. Будь-який виклик ApplyNow/ApplyBeforeLaunch (навіть на
// звичайному update- чи launch-шляху) читав/дописував цей маркер, і
// файл гравця міг злітати до дефолтів знову — при кожному оновленні
// збірки, а деколи і при кожному запуску гри. Маркер у options.txt
// прибрано повністю (mcoptions більше нічого не пише і не читає у
// самому файлі гри, крім видимих опцій) — перевірка "чи вже
// застосовано" робиться ЛИШЕ тут, ПЕРЕД викликом ApplyNow/
// ApplyBeforeLaunch, і ЛИШЕ за instance.json. MinecraftDefaultsApplied
// видаляється РАЗОМ з рештою конфігу збірки при видаленні (DeletePack →
// DeleteInstanceConfig) — тому "видалив збірку → перевстановив"
// коректно знову вважається першим запуском.
//
// ПОВНА СТРАХОВКА: навіть якщо прапорець MinecraftDefaultsApplied
// загубився (перенесена/імпортована збірка, помилка збереження
// instance.json тощо) — вже існуючий на диску options.txt дефолтами НЕ
// перезаписується: лаунчер лише позначає, що дефолти цій збірці більше
// не потрібні. Це остаточно закриває репорт «гра скидає налаштування
// (options.txt) при наступному запуску».
func (a *App) applyMinecraftDefaultsNow(packID string) {
	d := a.cfg.GetMinecraftDefaults()
	if !d.Enabled {
		return
	}
	if ic := a.cfg.GetInstanceConfig(packID); ic.MinecraftDefaultsApplied {
		log.Printf("mcdefaults: %s вже мала дефолти застосовані раніше (MinecraftDefaultsApplied=true) — пропускаю", packID)
		return
	}
	packDir := a.packDirForPack(packID)
	optionsFile := mcoptions.OptionsFilePath(packDir)
	// options.txt вже існує — це файл збірки або гравця (його могла
	// створити гра на попередній сесії, чи принести сама збірка). Такий
	// файл дефолтами НЕ перезаписуємо ніколи — саме це колись «скидало»
	// налаштування гравця при наступних запусках. Лише фіксуємо, що
	// дефолти цій збірці більше не потрібні.
	if _, err := os.Stat(optionsFile); err == nil {
		log.Printf("mcdefaults: %s вже має options.txt — дефолти не застосовую (файл гравця/збірки)", packID)
		a.markMinecraftDefaultsApplied(packID)
		return
	}
	// Тека гри створюється до застосування дефолтів (OptionsFilePath сам
	// додає .minecraft) — щоб options.txt писався у справжню теку гри.
	if err := os.MkdirAll(filepath.Join(packDir, ".minecraft"), 0755); err != nil {
		log.Printf("mcdefaults: створення теки %s: %v", packDir, err)
		return
	}
	if err := mcoptions.ApplyNow(packDir, d); err != nil {
		log.Printf("mcdefaults: застосування дефолтів до %s: %v", packID, err)
		return
	}
	a.markMinecraftDefaultsApplied(packID)
}

// markMinecraftDefaultsApplied записує в instance.json збірки прапорець
// MinecraftDefaultsApplied=true: «дефолти для цієї збірки більше не
// потрібні» (застосовані, або options.txt уже належить гравцю/збірці).
func (a *App) markMinecraftDefaultsApplied(packID string) {
	ic := a.cfg.GetInstanceConfig(packID)
	ic.ID = packID
	ic.MinecraftDefaultsApplied = true
	if err := a.cfg.SaveInstanceConfig(ic); err != nil {
		log.Printf("mcdefaults: не вдалося зберегти MinecraftDefaultsApplied для %s: %v", packID, err)
	}
}

// applyMinecraftDefaultsBeforeLaunch застосовує дефолти перед ПЕРШИМ
// запуском гри цієї збірки. Якщо вимкнено, або дефолти вже застосовували
// цій збірці раніше (instance.json.MinecraftDefaultsApplied) — нічого не
// робить: файл гравця для другого й наступних запусків не чіпається
// взагалі.
//
// РЕГРЕСІЯ (виправлено): раніше ApplyBeforeLaunch викликалась БЕЗУМОВНО
// на кожному запуску гри й сама розбиралась через маркер shaurma_pack у
// options.txt. Після видалення маркера з mcoptions виклик без цієї
// перевірки перезаписував би "видимі" опції гравця (fov, чутливість
// миші, бінди тощо) при КОЖНОМУ запуску — саме той симптом, на який
// скаржились гравці ("гра скидає й видаляє налаштування"). Тепер
// перевірка стоїть ПЕРЕД викликом, як і в applyMinecraftDefaultsNow —
// єдине джерело правди instance.json, файл гравця не чіпається вдруге.
// Додатково ця функція тепер перевіряє сам options.txt: файл, який уже
// є на диску, дефолтами не перезаписується навіть при втраченому
// прапорці (наприклад, збірка перенесена зі старого лаунчера, де файл
// вже створила гра).
func (a *App) applyMinecraftDefaultsBeforeLaunch(packID string) {
	d := a.cfg.GetMinecraftDefaults()
	if !d.Enabled {
		return
	}
	if ic := a.cfg.GetInstanceConfig(packID); ic.MinecraftDefaultsApplied {
		return
	}
	packDir := a.packDirForPack(packID)
	// options.txt вже є на диску (його створила гра під час попередньої
	// сесії, принесла збірка чи сам гравець) — це файл гравця/збірки: при
	// запуску не чіпаємо. Раніше ApplyBeforeLaunch викликалась тут
	// безумовно й перезаписувала «видимі» опції гравця при кожному
	// запуску, коли прапорець MinecraftDefaultsApplied губився — тепер
	// наявний options.txt завжди означає «дефолти вже не потрібні».
	if _, err := os.Stat(mcoptions.OptionsFilePath(packDir)); err == nil {
		log.Printf("mcdefaults: %s вже має options.txt перед запуском — дефолти не застосовую (файл гравця/збірки)", packID)
		a.markMinecraftDefaultsApplied(packID)
		return
	}
	// options.txt ще немає (гра жодного разу не запускалась, а дефолти не
	// встигли застосуватись при встановленні) — створюємо з дефолтів.
	mcoptions.ApplyBeforeLaunch(packDir, d)
	a.markMinecraftDefaultsApplied(packID)
}
