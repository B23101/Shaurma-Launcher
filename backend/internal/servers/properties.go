// properties.go — парсер/редактор server.properties зі збереженням
// коментарів та порядку ключів.
//
// Логіка й причини рішень перенесені з GDLauncher Carbon
// (crates/carbon_app/src/managers/server/properties.rs): дефолтна
// генерація файлу з pre-1.14 watchdog-фіксом, захист від newline-injection
// в update_properties, і збереження чужих коментарів/порядку ключів.
package servers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PropEntry — один ключ server.properties разом з метаданими зі схеми.
type PropEntry struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Comment string   `json:"comment,omitempty"`
	Type    string   `json:"type"` // int|bool|string|enum
	Enum    []string `json:"enum,omitempty"`
	Min     *int64   `json:"min,omitempty"`
	Max     *int64   `json:"max,omitempty"`
	Hint    string   `json:"hint,omitempty"`
	Default string   `json:"default,omitempty"`

	// Group — id групи (props.group.<id> в i18n). Для ключів поза схемою
	// (їх дописують моди/плагіни) — "other".
	Group string `json:"group,omitempty"`
	// Known — ключ описано в схемі. Невідомі ключі UI показує окремо.
	Known bool `json:"known"`
	// Unit — одиниця виміру для підпису поруч з полем ("чанків", "мс"...).
	Unit string `json:"unit,omitempty"`
	// Slider — для вузьких int-діапазонів показувати повзунок; Step — його крок.
	Slider bool `json:"slider,omitempty"`
	Step   int  `json:"step,omitempty"`
	// Secret — значення маскується в UI (паролі, токени).
	Secret bool `json:"secret,omitempty"`
	// NewWorldOnly — читається лише при СТВОРЕННІ світу; на вже згенерованому
	// світі зміна нічого не дає.
	NewWorldOnly bool `json:"newWorldOnly,omitempty"`
	// Open — enum, який дозволяє власні значення (плагіни додають типи світу).
	Open bool `json:"open,omitempty"`
	// Since/Until — версії гри, у яких ключ з'явився / був прибраний.
	Since string `json:"since,omitempty"`
	Until string `json:"until,omitempty"`
	// Status — "removed"/"new", якщо ключ не діє в версії цього сервера.
	Status string `json:"status,omitempty"`
	// Order — позиція у схемі (1..N); 0 для невідомих ключів. UI сортує за нею,
	// щоб параметри йшли як у довіднику, а не за абеткою файлу.
	Order int `json:"order,omitempty"`
}

// PropertiesDoc — документ server.properties.
type PropertiesDoc struct {
	Entries   []PropEntry `json:"entries"`
	Raw       string      `json:"raw"`
	McVersion string      `json:"mcVersion,omitempty"`
}

// ReadServerProperties читає server.properties і повертає структурований
// документ. Якщо файл ще не існує (сервер жодного разу не запускався —
// java сам створює server.properties лише при першому старті, а тепер
// install/старт відкладені до кнопки "Старт"), генерує і зберігає дефолтний
// файл зі схеми PropertiesSchema, щоб порт та інші налаштування можна було
// редагувати ДО першого запуску, а не чекати на нього.
//
// Публічна сигнатура (serverDir string) — жорсткий контракт з api.go,
// тому версія MC тут невідома: дефолтний файл генерується БЕЗ pre-1.14
// watchdog-фіксу (розділ 3.1). ReadServerPropertiesForVersion нижче —
// та сама функція, що приймає версію і застосовує фікс; api.go наразі її
// не викликає (див. звіт по правках, пункт "потребує нового поля/виклику").
func ReadServerProperties(serverDir string) (PropertiesDoc, error) {
	return ReadServerPropertiesForVersion(serverDir, "")
}

// ReadServerPropertiesForVersion — те саме, що ReadServerProperties, але
// приймає mcVersion сервера, щоб генерація дефолтного файлу (якщо його ще
// немає) могла застосувати pre-1.14 watchdog-фікс (розділ 3.1). Порожній
// mcVersion веде себе так само, як ReadServerProperties: фікс не
// застосовується (isPre114 повертає false для порожнього рядка).
func ReadServerPropertiesForVersion(serverDir, mcVersion string) (PropertiesDoc, error) {
	path := filepath.Join(serverDir, "server.properties")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return PropertiesDoc{}, fmt.Errorf("читання server.properties: %w", err)
		}
		if genErr := writeDefaultServerProperties(serverDir, mcVersion); genErr != nil {
			return PropertiesDoc{}, fmt.Errorf("створення server.properties: %w", genErr)
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return PropertiesDoc{}, fmt.Errorf("читання server.properties: %w", err)
		}
	}

	entries := parsePropEntries(string(data), mcVersion)

	return PropertiesDoc{
		Entries:   entries,
		Raw:       string(data),
		McVersion: mcVersion,
	}, nil
}

var plainIntRe = regexp.MustCompile(`^-?[1-9][0-9]{0,14}$|^0$`)

// parsePropEntries розбирає вміст файлу в записи, збагачені схемою.
//
// Дублікати ключів зливаємо за правилом java.util.Properties — виграє
// ОСТАННЄ значення, а позиція лишається за першим входженням. Раніше кожен
// дубль ставав окремим рядком у UI, а keyed-each у Svelte на повторному
// ключі ламав усю сторінку.
func parsePropEntries(content, mcVersion string) []PropEntry {
	var entries []PropEntry
	index := map[string]int{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var pendingComment string

	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())

		// Коментар або порожній рядок.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			if trimmed != "" {
				pendingComment = trimmed
			}
			continue
		}

		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := strings.TrimSpace(trimmed[idx+1:])
		if key == "" {
			continue
		}

		if i, dup := index[key]; dup {
			entries[i].Value = val
			continue
		}

		entry := PropEntry{Key: key, Value: val, Comment: pendingComment}
		if schema, ok := PropertiesSchema[key]; ok {
			entry = schema
			entry.Value = val
			entry.Comment = pendingComment
			entry.Default = defaultFor(schema, mcVersion)
			entry.Status = PropStatus(schema, mcVersion)
		} else {
			// Ключ поза схемою (мод/плагін або новіша гра). Тип виводимо зі
			// значення, щоб "true"/"false" не малювалось текстовим полем.
			entry.Group = PropGroupOther
			switch {
			case val == "true" || val == "false":
				entry.Type = "bool"
			case plainIntRe.MatchString(val):
				entry.Type = "int"
			default:
				entry.Type = "string"
			}
		}
		index[key] = len(entries)
		entries = append(entries, entry)
		pendingComment = ""
	}
	return entries
}

// isPre114 повідомляє, чи випускова версія MC (напр. "1.12.2") нижча за
// 1.14. Перенесено з properties.rs::is_pre_1_14 (рядки 48-59): снапшоти й
// інші нечислові рядки ("18w50a", "unknown") повертають false — фікс
// застосовується лише коли версія точно відома і точно стара.
func isPre114(mcVersion string) bool {
	parts := strings.SplitN(mcVersion, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major == 1 && minor < 14
}

// writeDefaultServerProperties генерує server.properties з дефолтних
// значень PropertiesSchema, у стабільному порядку (сортування за ключем,
// щоб файл був детермінованим і не змінювався від запуску до запуску до
// того, як його вперше перепише сам java-процес).
//
// pre-1.14 watchdog fix (properties.rs рядки 34-39, generate_properties):
// версії Minecraft нижче 1.14 регулярно перевищують 60-секундний ліміт
// watchdog-а під час генерації світу при першому запуску, і ванільний
// watchdog у такому разі вбиває сервер. Якщо mcVersion < 1.14, ставимо
// max-tick-time=-1 (вимикаємо watchdog) замість дефолтного значення зі
// схеми — перекриваємо схему точково, не змінюючи саму PropertiesSchema.
func writeDefaultServerProperties(serverDir, mcVersion string) error {
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		return err
	}
	disableWatchdog := isPre114(mcVersion)

	var b strings.Builder
	b.WriteString("#Minecraft server properties\n")
	b.WriteString("#Згенеровано лаунчером — буде доповнено/оновлено сервером при першому запуску\n")
	// Порядок — за схемою (а не за алфавітом), ключі, які не діють у цій
	// версії гри, у файл не пишемо.
	for _, e := range schemaList {
		if PropStatus(e, mcVersion) != "" {
			continue
		}
		// secret генерує сам сервер; generator-settings="{}" для старих версій
		// не годиться — лишаємо порожнім, як було завжди.
		if e.Key == "management-server-secret" {
			continue
		}
		val := e.Default
		if e.Key == "generator-settings" {
			val = ""
		}
		if e.Key == "max-tick-time" && disableWatchdog {
			val = "-1"
		}
		b.WriteString(e.Key)
		b.WriteString("=")
		b.WriteString(val)
		b.WriteString("\n")
	}
	return writeFileAtomic(filepath.Join(serverDir, "server.properties"), []byte(b.String()), 0644)
}

// hasControlChars повідомляє, чи рядок містить сирий '\n' або '\r'.
// Перенесено з properties.rs::has_control_chars (рядки 7-16): '\n'
// всередині значення перетворює один рядок "ключ=значення" на два після
// об'єднання рядків файлу, дозволяючи інʼєктувати довільний ДРУГИЙ рядок
// властивості (напр. значення "Hacked\r\nonline-mode=false" для motd
// тихцем вимикає Mojang-автентифікацію через підроблений другий рядок).
// Межа API (SaveServerProperties у api.go) має відкидати такі значення ще
// раніше — це останній рубіж захисту для того, що дійшло до формату файлу.
func hasControlChars(s string) bool {
	return strings.ContainsAny(s, "\n\r")
}

// WriteServerProperties оновлює вказані ключі, зберігаючи коментарі й
// порядок наявних рядків, дописуючи в кінець ключі, яких у файлі ще не
// було, і атомарно записуючи результат на диск. Якщо server.properties ще
// не існує, спершу генерує дефолтний файл (той самий випадок, що в
// ReadServerProperties) — інакше збереження налаштувань до першого запуску
// сервера падало б з помилкою читання.
//
// Дві незалежні перевірки застосовуються до кожного PropUpdate, і в них
// СВІДОМО різна поведінка при провалі:
//   - ValidateProperty (тип/діапазон значення — число поза межами схеми
//     тощо) — це помилка користувача. Весь виклик відкидається одразу,
//     нічого не записується: усе-або-нічого, як і раніше.
//   - hasControlChars (структурна цілісність файлу, розділ 3.2) — це
//     підозріла аномалія ОДНОГО конкретного значення, а не помилка
//     користувача per se (значення могло прийти не з форми UI, а з
//     modpack-манфесту чи іншого зовнішнього джерела). Такий update тихо
//     відкидається з попередженням у лог, а решта updates з того самого
//     виклику застосовуються нормально — це НЕ all-or-nothing.
func WriteServerProperties(serverDir string, updates []PropUpdate) error {
	path := filepath.Join(serverDir, "server.properties")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("читання server.properties: %w", err)
		}
		if genErr := writeDefaultServerProperties(serverDir, ""); genErr != nil {
			return fmt.Errorf("створення server.properties: %w", genErr)
		}
		data, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("читання server.properties: %w", err)
		}
	}

	// Крок 1: ValidateProperty — тип/діапазон. Помилка користувача:
	// усе-або-нічого, нічого не записуємо, повертаємо помилку одразу.
	for _, u := range updates {
		if err := ValidateProperty(u.Key, u.Value); err != nil {
			return fmt.Errorf("невалідне значення %s: %w", u.Key, err)
		}
	}

	// Крок 2: newline-injection — структурна аномалія одного значення.
	// Відкидаємо конкретний update з попередженням, інші йдуть далі.
	updateMap := make(map[string]string, len(updates))
	for _, u := range updates {
		if hasControlChars(u.Key) || hasControlChars(u.Value) {
			log.Printf("[servers] відкинуто server.properties ключ %q: значення або ключ містить newline (можлива інʼєкція)", u.Key)
			continue
		}
		updateMap[u.Key] = u.Value
	}

	// Рядкове оновлення наявних рядків.
	lines := strings.Split(string(data), "\n")
	updatedKeys := make(map[string]bool, len(updateMap))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "!") {
			continue
		}
		idx := strings.IndexByte(trimmed, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		if newVal, ok := updateMap[key]; ok {
			lines[i] = key + "=" + newVal
			updatedKeys[key] = true
		}
	}

	// Дописуємо ключі, яких у файлі ще не було.
	for _, u := range updates {
		if updatedKeys[u.Key] {
			continue
		}
		newVal, ok := updateMap[u.Key]
		if !ok {
			continue // відкинуто на кроці 2 (newline-injection)
		}
		lines = append(lines, u.Key+"="+newVal)
	}

	return writeFileAtomic(path, []byte(strings.Join(lines, "\n")), 0644)
}

// WriteServerPropertiesForVersion — те саме, що WriteServerProperties, але
// генерує дефолтний файл (якщо server.properties ще не існує) з
// урахуванням pre-1.14 watchdog-фіксу для mcVersion. Додаткова функція з
// тих самих причин, що ReadServerPropertiesForVersion вище.
func WriteServerPropertiesForVersion(serverDir, mcVersion string, updates []PropUpdate) error {
	path := filepath.Join(serverDir, "server.properties")
	if !fileExists(path) {
		if genErr := writeDefaultServerProperties(serverDir, mcVersion); genErr != nil {
			return fmt.Errorf("створення server.properties: %w", genErr)
		}
	}
	return WriteServerProperties(serverDir, updates)
}

// PropUpdate — оновлення одного ключа.
//
// Value свідомо вміє приймати НЕ лише рядок: Svelte-бінд на
// <input type="number"> та <input type="range"> віддає JS-число, тому
// JSON ішов як {"value": 25565} і бекенд падав із
// "cannot unmarshal number into Go struct field PropUpdate.value of type
// string" (навіть для звичайного збереження властивостей). Тепер число/
// bool/null нормалізуються в рядок тут — форма редагування, манфести
// модпаків і будь-який інший джерело працюють однаково.
type PropUpdate struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// UnmarshalJSON приймає value як рядок, число, bool або null.
func (u *PropUpdate) UnmarshalJSON(data []byte) error {
	var raw struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.Key = raw.Key
	if len(raw.Value) == 0 || string(raw.Value) == "null" {
		u.Value = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(raw.Value, &s); err == nil {
		u.Value = s
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(raw.Value, &n); err == nil {
		u.Value = n.String()
		return nil
	}
	var b bool
	if err := json.Unmarshal(raw.Value, &b); err == nil {
		u.Value = strconv.FormatBool(b)
		return nil
	}
	// Невідомий тип — чесна помилка, а не тихий мовчазний провал.
	return fmt.Errorf("значення властивості %q має бути рядком або числом", raw.Key)
}

// ValidateProperty валідує значення ключа server.properties.
func ValidateProperty(key, value string) error {
	schema, ok := PropertiesSchema[key]
	if !ok {
		return nil // невідомий ключ — пропускаємо
	}

	switch schema.Type {
	case "int":
		// int64: max-tick-time та інші можуть бути більшими за int32.
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return fmt.Errorf("очікується число: %s", key)
		}
		if schema.Min != nil && n < *schema.Min {
			return fmt.Errorf("мінімум: %d", *schema.Min)
		}
		if schema.Max != nil && n > *schema.Max {
			return fmt.Errorf("максимум: %d", *schema.Max)
		}
	case "bool":
		if value != "true" && value != "false" {
			return fmt.Errorf("очікується true або false")
		}
	case "enum":
		if schema.Open || len(schema.Enum) == 0 {
			return nil // власні значення дозволені (напр. типи світу з плагінів)
		}
		for _, e := range schema.Enum {
			if e == value {
				return nil
			}
		}
		return fmt.Errorf("допустимі значення: %v", schema.Enum)
	}
	return nil
}

// writeFileAtomic пише data в path через тимчасовий файл у тій самій
// директорії + os.Rename, замість прямого os.WriteFile.
//
// ПОНАД ОРИГІНАЛ: GDLauncher-івський write_properties (properties.rs,
// tokio::fs::write) сам НЕ атомарний — пряме tokio::fs::write() так само,
// як os.WriteFile, може лишити файл обрізаним, якщо процес впаде рівно
// посеред запису. Це не перенесене з джерела рішення, а окреме посилення:
// у пакеті вже є internal/atomicfile.WriteJSONAtomic для JSON-файлів
// (settings.json, accounts.json тощо) з тим самим обґрунтуванням — тут
// той самий підхід (tmp-файл поруч + rename на тій же файловій системі,
// що робить заміну атомарною) застосовано до текстового
// server.properties, якого atomicfile напряму не покриває (не JSON).
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
