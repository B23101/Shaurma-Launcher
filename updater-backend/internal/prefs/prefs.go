// Package prefs читає налаштування ЛАУНЧЕРА (не апдейтера) самостійно з
// JSON-файлу (shaurma.json у installDir), а не покладається на те, що
// лаунчер передасть «правильні» прапорці. Тільки --channel (edition:
// shaurma/base, вшитий у launcher.exe build tag'ом) приходить ЗЗОВНІ явно,
// бо це не налаштування користувача, а факт про те, який бінарник його
// викликав.
//
// AutoUpdate і UpdateChannel (stable/beta) — навпаки, ЗАВЖДИ читаються
// апдейтером самостійно з JSON. Якщо файлу/запису немає (перший запуск,
// launcher.exe ще жодного разу не стартував, або updater запущено напряму
// вручну) — дефолт: AutoUpdate=true, UpdateChannel="stable".
package prefs

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// JSONFileName — ім'я JSON-файлу даних лаунчера (поруч із DLL).
const JSONFileName = "shaurma.json"

// globalData — спрощена структура для читання settings з JSON.
type globalData struct {
	Settings string `json:"settings,omitempty"`
}

// Prefs — підмножина налаштувань лаунчера, потрібна апдейтеру.
type Prefs struct {
	AutoUpdate    bool
	UpdateChannel string // "stable" | "beta"
}

// Defaults — застосовується, якщо JSON немає, рядок налаштувань відсутній
// або битий.
func Defaults() Prefs {
	return Prefs{AutoUpdate: true, UpdateChannel: "stable"}
}

// Load читає settings з JSON-файлу лаунчера в installDir. Будь-яка помилка
// (файл відсутній, рядка немає, битий JSON-блоб) тихо повертає Defaults() —
// апдейтер не повинен падати чи блокуватись через відсутність/пошкодження
// налаштувань лаунчера, це не критичний файл для самого апдейтера.
func Load(installDir string) Prefs {
	p := Defaults()

	value, ok := Raw(installDir)
	if !ok {
		return p
	}

	var raw struct {
		AutoUpdate    *bool  `json:"autoUpdate"`
		UpdateChannel string `json:"updateChannel"`
	}
	if json.Unmarshal([]byte(value), &raw) != nil {
		return p
	}

	if raw.AutoUpdate != nil {
		p.AutoUpdate = *raw.AutoUpdate
	}
	if raw.UpdateChannel == "stable" || raw.UpdateChannel == "beta" {
		p.UpdateChannel = raw.UpdateChannel
	}
	// Будь-яке інше/порожнє значення updateChannel (майбутні канали, битий
	// запис) — лишаємо дефолтний "stable": краще показати перевірений
	// реліз, ніж мовчки впасти в невідомий канал.
	return p
}

// Raw повертає сирий JSON-блоб налаштувань лаунчера з shaurma.json (без
// парсингу). Другим значенням — чи запис знайдено. Використовується і
// Load(), і main.go для мови/акценту/теми splice-екрана.
func Raw(installDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(installDir, JSONFileName))
	if err != nil {
		// Фолбек: пробуємо старий shaurma.db (сумісність при першому запуску
		// після оновлення, поки лаунчер ще не переніс БД в JSON).
		return rawFromDB(installDir)
	}

	var doc globalData
	if json.Unmarshal(data, &doc) != nil {
		return rawFromDB(installDir)
	}
	if doc.Settings == "" {
		return rawFromDB(installDir)
	}
	return doc.Settings, true
}

// rawFromDB — фолбек: читає settings зі старого shaurma.db (сумісність).
func rawFromDB(installDir string) (string, bool) {
	// Якщо shaurma.db не існує — нічого.
	dbPath := filepath.Join(installDir, "shaurma.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", false
	}

	// Відкриваємо БД напряму без зовнішньої залежності від модуля лаунчера.
	// Тут потрібен database/sql + sqlite driver.
	return readSettingsFromDB(dbPath)
}
