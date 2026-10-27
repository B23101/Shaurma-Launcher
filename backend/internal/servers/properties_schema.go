// properties_schema.go — повна схема server.properties (Java Edition).
//
// Джерело: офіційна Minecraft Wiki (minecraft.wiki/w/Server.properties),
// звірена з файлом, який генерує чистий сервер 26.3 (69 ключів).
// Довідник: https://www.gameserverkings.com/knowledge-base/minecraft/server-properties-reference/
//
// Що тут навмисно інакше, ніж було раніше:
//   - РАНІШЕ схема знала лише ~38 ключів із 69, решта малювалась у UI
//     текстовим полем з "true"/"false". Тепер описано ВСІ.
//   - Ліміти виправлено за вікі: view-/simulation-distance 3..32 (було 2),
//     op-permission-level 0..4 (було 1..4), порти 1..65534 (було 65535),
//     max-players 0..2^31-1 (було 1..1000), rate-limit без верхньої межі.
//   - Групи (Group) тепер у схемі, а не в окремому списку на фронтенді —
//     два списки розходились, і нові ключі потрапляли в «Інше».
//   - Ключі, які Mojang прибрала (pvp → /gamerule pvp тощо), мають Until:
//     для нових версій вони позначаються як «не діють» і не пишуться в
//     згенерований файл.
//   - server-name тут немає: це ключ Bedrock, у Java його не існує.
package servers

import (
	"math"
	"strconv"
	"strings"
)

// Ідентифікатори груп (UI бере підписи з i18n: props.group.<id>).
const (
	PropGroupPlayers     = "players"
	PropGroupWorld       = "world"
	PropGroupGameplay    = "gameplay"
	PropGroupStatus      = "status"
	PropGroupNetwork     = "network"
	PropGroupPerformance = "performance"
	PropGroupRemote      = "remote"
	PropGroupManagement  = "management"
	PropGroupResources   = "resources"
	PropGroupLogging     = "logging"
	PropGroupOther       = "other"
)

func i64(n int64) *int64 { return &n }

// schemaList — порядок тут = порядок показу всередині групи.
var schemaList = []PropEntry{
	// ── Гравці та доступ ────────────────────────────────────────────
	{Key: "max-players", Group: PropGroupPlayers, Type: "int", Min: i64(0), Max: i64(math.MaxInt32), Default: "20", Hint: "Максимум гравців одночасно"},
	{Key: "white-list", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Білий список"},
	{Key: "enforce-whitelist", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Викидати гравців не зі списку"},
	{Key: "online-mode", Group: PropGroupPlayers, Type: "bool", Default: "true", Hint: "Перевірка облікових записів Mojang"},
	{Key: "enforce-secure-profile", Group: PropGroupPlayers, Type: "bool", Default: "true", Hint: "Вимагати підписаний ключ Mojang"},
	{Key: "prevent-proxy-connections", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Блокувати проксі/VPN"},
	{Key: "accepts-transfers", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Приймати трансфери з інших серверів"},
	{Key: "op-permission-level", Group: PropGroupPlayers, Type: "enum", Enum: []string{"0", "1", "2", "3", "4"}, Default: "4", Hint: "Рівень нових операторів"},
	{Key: "function-permission-level", Group: PropGroupPlayers, Type: "enum", Enum: []string{"1", "2", "3", "4"}, Default: "2", Hint: "Рівень команд у функціях датапаків"},
	{Key: "allow-flight", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Дозволити політ у виживанні"},
	{Key: "player-idle-timeout", Group: PropGroupPlayers, Type: "int", Min: i64(0), Unit: "хв", Default: "0", Hint: "Кік за бездіяльність (0 = вимкнено)"},
	{Key: "rate-limit", Group: PropGroupPlayers, Type: "int", Min: i64(0), Default: "0", Hint: "Ліміт пакетів від гравця (0 = вимкнено)"},
	{Key: "chat-spam-threshold-seconds", Group: PropGroupPlayers, Type: "int", Min: i64(0), Default: "10", Since: "26.2", Hint: "Ліміт чату до кіку за спам (0 = вимкнено)"},
	{Key: "command-spam-threshold-seconds", Group: PropGroupPlayers, Type: "int", Min: i64(0), Default: "10", Since: "26.2", Hint: "Ліміт команд до кіку за спам (0 = вимкнено)"},
	{Key: "enable-code-of-conduct", Group: PropGroupPlayers, Type: "bool", Default: "false", Hint: "Показувати правила при вході"},

	// ── Світ ────────────────────────────────────────────────────────
	{Key: "level-name", Group: PropGroupWorld, Type: "string", Default: "world", Hint: "Назва папки зі світом"},
	{Key: "level-seed", Group: PropGroupWorld, Type: "string", Default: "", NewWorldOnly: true, Hint: "Сід (порожньо = випадковий)"},
	{Key: "level-type", Group: PropGroupWorld, Type: "enum", Open: true, NewWorldOnly: true,
		Enum:    []string{"minecraft\\:normal", "minecraft\\:flat", "minecraft\\:large_biomes", "minecraft\\:amplified", "minecraft\\:single_biome_surface"},
		Default: "minecraft\\:normal", Hint: "Тип світу"},
	{Key: "generator-settings", Group: PropGroupWorld, Type: "string", Default: "{}", NewWorldOnly: true, Hint: "JSON налаштувань генератора"},
	{Key: "generate-structures", Group: PropGroupWorld, Type: "bool", Default: "true", Hint: "Генерувати структури"},
	{Key: "max-world-size", Group: PropGroupWorld, Type: "int", Min: i64(1), Max: i64(29999984), Unit: "блоків", Default: "29999984", Hint: "Радіус світу від центру"},
	{Key: "hardcore", Group: PropGroupWorld, Type: "bool", Default: "false", Hint: "Хардкор"},
	{Key: "initial-enabled-packs", Group: PropGroupWorld, Type: "string", Default: "vanilla", NewWorldOnly: true, Hint: "Датапаки, увімкнені при створенні світу"},
	{Key: "initial-disabled-packs", Group: PropGroupWorld, Type: "string", Default: "", NewWorldOnly: true, Hint: "Датапаки, вимкнені при створенні світу"},

	// ── Геймплей ────────────────────────────────────────────────────
	{Key: "gamemode", Group: PropGroupGameplay, Type: "enum", Enum: []string{"survival", "creative", "adventure", "spectator"}, Default: "survival", Hint: "Режим гри за замовчуванням"},
	{Key: "force-gamemode", Group: PropGroupGameplay, Type: "bool", Default: "false", Hint: "Скидати режим гри при кожному вході"},
	{Key: "difficulty", Group: PropGroupGameplay, Type: "enum", Enum: []string{"peaceful", "easy", "normal", "hard"}, Default: "easy", Hint: "Складність"},
	{Key: "spawn-protection", Group: PropGroupGameplay, Type: "int", Min: i64(0), Unit: "блоків", Default: "16", Hint: "Радіус захисту спавну"},
	// Прибрані з server.properties — у нових версіях це правила гри.
	{Key: "pvp", Group: PropGroupGameplay, Type: "bool", Default: "true", Until: "1.21.9", Hint: "PvP"},
	{Key: "enable-command-block", Group: PropGroupGameplay, Type: "bool", Default: "false", Until: "1.21.9", Hint: "Командні блоки"},
	{Key: "allow-nether", Group: PropGroupGameplay, Type: "bool", Default: "true", Until: "1.21.9", Hint: "Незер"},
	{Key: "spawn-monsters", Group: PropGroupGameplay, Type: "bool", Default: "true", Until: "1.21.9", Hint: "Спавн монстрів"},
	{Key: "spawn-animals", Group: PropGroupGameplay, Type: "bool", Default: "true", Until: "1.21.2", Hint: "Спавн тварин"},
	{Key: "spawn-npcs", Group: PropGroupGameplay, Type: "bool", Default: "true", Until: "1.21.2", Hint: "Спавн жителів"},

	// ── Список серверів ─────────────────────────────────────────────
	{Key: "motd", Group: PropGroupStatus, Type: "string", Default: "A Minecraft Server", Hint: "Опис під назвою сервера (до 59 символів)"},
	{Key: "enable-status", Group: PropGroupStatus, Type: "bool", Default: "true", Hint: "Відповідати на пінги списку серверів"},
	{Key: "hide-online-players", Group: PropGroupStatus, Type: "bool", Default: "false", Hint: "Ховати імена гравців у списку"},
	{Key: "bug-report-link", Group: PropGroupStatus, Type: "string", Default: "", Hint: "Посилання на баг-репорти"},

	// ── Мережа ──────────────────────────────────────────────────────
	{Key: "server-port", Group: PropGroupNetwork, Type: "int", Min: i64(1), Max: i64(65534), Default: "25565", Hint: "TCP-порт сервера"},
	{Key: "server-ip", Group: PropGroupNetwork, Type: "string", Default: "", Hint: "IP для прив'язки (порожньо = усі)"},
	{Key: "network-compression-threshold", Group: PropGroupNetwork, Type: "int", Min: i64(-1), Unit: "байт", Default: "256", Hint: "Поріг стиснення пакетів (-1 = вимкнено)"},
	{Key: "use-native-transport", Group: PropGroupNetwork, Type: "bool", Default: "true", Hint: "Нативний мережевий транспорт (Linux)"},

	// ── Продуктивність ──────────────────────────────────────────────
	{Key: "view-distance", Group: PropGroupPerformance, Type: "int", Min: i64(3), Max: i64(32), Slider: true, Unit: "чанків", Default: "10", Hint: "Дальність видимості"},
	{Key: "simulation-distance", Group: PropGroupPerformance, Type: "int", Min: i64(3), Max: i64(32), Slider: true, Unit: "чанків", Default: "10", Hint: "Дальність симуляції"},
	{Key: "entity-broadcast-range-percentage", Group: PropGroupPerformance, Type: "int", Min: i64(10), Max: i64(1000), Slider: true, Step: 5, Unit: "%", Default: "100", Hint: "Дальність показу сутностей"},
	{Key: "max-tick-time", Group: PropGroupPerformance, Type: "int", Min: i64(-1), Unit: "мс", Default: "60000", Hint: "Таймаут watchdog (-1 = вимкнено)"},
	{Key: "max-chained-neighbor-updates", Group: PropGroupPerformance, Type: "int", Default: "1000000", Hint: "Ліміт ланцюгових оновлень блоків"},
	{Key: "pause-when-empty-seconds", Group: PropGroupPerformance, Type: "int", Unit: "с", Default: "60", Hint: "Пауза тіків, коли немає гравців"},
	{Key: "sync-chunk-writes", Group: PropGroupPerformance, Type: "bool", Default: "true", Hint: "Синхронний запис чанків"},
	{Key: "region-file-compression", Group: PropGroupPerformance, Type: "enum", Enum: []string{"deflate", "lz4", "none"}, Default: "deflate", Hint: "Стиснення файлів регіонів"},

	// ── Віддалений доступ ───────────────────────────────────────────
	{Key: "enable-rcon", Group: PropGroupRemote, Type: "bool", Default: "false", Hint: "RCON"},
	{Key: "rcon.port", Group: PropGroupRemote, Type: "int", Min: i64(1), Max: i64(65534), Default: "25575", Hint: "Порт RCON"},
	{Key: "rcon.password", Group: PropGroupRemote, Type: "string", Secret: true, Default: "", Hint: "Пароль RCON"},
	{Key: "broadcast-rcon-to-ops", Group: PropGroupRemote, Type: "bool", Default: "true", Hint: "Показувати вивід RCON операторам"},
	{Key: "enable-query", Group: PropGroupRemote, Type: "bool", Default: "false", Hint: "Query-протокол (UDP)"},
	{Key: "query.port", Group: PropGroupRemote, Type: "int", Min: i64(1), Max: i64(65534), Default: "25565", Hint: "UDP-порт query"},
	{Key: "enable-jmx-monitoring", Group: PropGroupRemote, Type: "bool", Default: "false", Hint: "JMX-моніторинг"},

	// ── API керування (Management Protocol) ─────────────────────────
	{Key: "management-server-enabled", Group: PropGroupManagement, Type: "bool", Default: "false", Since: "1.21.9", Hint: "Увімкнути API керування"},
	{Key: "management-server-host", Group: PropGroupManagement, Type: "string", Default: "localhost", Since: "1.21.9", Hint: "Хост API"},
	{Key: "management-server-port", Group: PropGroupManagement, Type: "int", Min: i64(0), Max: i64(65534), Default: "0", Since: "1.21.9", Hint: "Порт API (0 = випадковий)"},
	{Key: "management-server-secret", Group: PropGroupManagement, Type: "string", Secret: true, Default: "", Since: "1.21.9", Hint: "Токен доступу (40 символів)"},
	{Key: "management-server-allowed-origins", Group: PropGroupManagement, Type: "string", Default: "", Since: "1.21.9", Hint: "Дозволені Origin через кому"},
	{Key: "management-server-tls-enabled", Group: PropGroupManagement, Type: "bool", Default: "true", Since: "1.21.9", Hint: "TLS для API"},
	{Key: "management-server-tls-keystore", Group: PropGroupManagement, Type: "string", Default: "", Since: "1.21.9", Hint: "Шлях до сховища ключів (PKCS12)"},
	{Key: "management-server-tls-keystore-password", Group: PropGroupManagement, Type: "string", Secret: true, Default: "", Since: "1.21.9", Hint: "Пароль сховища ключів"},
	{Key: "status-heartbeat-interval", Group: PropGroupManagement, Type: "int", Min: i64(0), Default: "0", Since: "1.21.9", Hint: "Інтервал heartbeat (0 = вимкнено)"},

	// ── Ресурс-пак ──────────────────────────────────────────────────
	{Key: "resource-pack", Group: PropGroupResources, Type: "string", Default: "", Hint: "Пряме посилання на ресурс-пак (до 250 МіБ)"},
	{Key: "resource-pack-sha1", Group: PropGroupResources, Type: "string", Default: "", Hint: "SHA-1 ресурс-паку"},
	{Key: "resource-pack-id", Group: PropGroupResources, Type: "string", Default: "", Hint: "UUID ресурс-паку"},
	{Key: "resource-pack-prompt", Group: PropGroupResources, Type: "string", Default: "", Hint: "Повідомлення в запиті ресурс-паку"},
	{Key: "require-resource-pack", Group: PropGroupResources, Type: "bool", Default: "false", Hint: "Кікати тих, хто відмовився"},

	// ── Журнал і консоль ────────────────────────────────────────────
	{Key: "log-ips", Group: PropGroupLogging, Type: "bool", Default: "true", Hint: "Писати IP гравців у журнал"},
	{Key: "broadcast-console-to-ops", Group: PropGroupLogging, Type: "bool", Default: "true", Hint: "Показувати вивід консолі операторам"},
	{Key: "text-filtering-config", Group: PropGroupLogging, Type: "string", Default: "", Hint: "Службовий (Realms)"},
	{Key: "text-filtering-version", Group: PropGroupLogging, Type: "enum", Enum: []string{"0", "1"}, Default: "0", Hint: "Службовий (Realms)"},
}

// PropertiesSchema — схема за ключем (для швидкого пошуку й валідації).
var PropertiesSchema = buildSchema()

func buildSchema() map[string]PropEntry {
	m := make(map[string]PropEntry, len(schemaList))
	for i, e := range schemaList {
		e.Known = true
		e.Order = i + 1
		m[e.Key] = e
	}
	return m
}

// ── Версії Minecraft ────────────────────────────────────────────────────

// parseMCVersion розбирає "1.21.9" / "26.3" / "1.21.9-rc1" у числа.
// ok=false для порожніх рядків і снапшотів ("24w14a"): за невідомої версії
// ми нічого не ховаємо й не вигадуємо.
func parseMCVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, false
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		end := 0
		for end < len(p) && p[end] >= '0' && p[end] <= '9' {
			end++
		}
		if end == 0 {
			return nil, false
		}
		// "24w14a" → число 24, але далі буква — це снапшот, а не реліз.
		if end < len(p) && len(out) == 0 {
			return nil, false
		}
		n, err := strconv.Atoi(p[:end])
		if err != nil {
			return nil, false
		}
		out = append(out, n)
		if end < len(p) {
			break // суфікс на кшталт "-rc1": далі не розбираємо
		}
	}
	return out, len(out) >= 2
}

// cmpMCVersion: -1 / 0 / 1; ok=false якщо хоч одну версію не розібрано.
func cmpMCVersion(a, b string) (int, bool) {
	pa, okA := parseMCVersion(a)
	pb, okB := parseMCVersion(b)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1, true
		}
		if x > y {
			return 1, true
		}
	}
	return 0, true
}

// PropStatus повідомляє, чи діє ключ у версії гри:
//
//	""        — діє (або версія невідома);
//	"removed" — Mojang прибрала ключ (Until ≤ версії);
//	"new"     — ключ з'явився пізніше (Since > версії).
func PropStatus(e PropEntry, mcVersion string) string {
	if e.Until != "" {
		if c, ok := cmpMCVersion(mcVersion, e.Until); ok && c >= 0 {
			return "removed"
		}
	}
	if e.Since != "" {
		if c, ok := cmpMCVersion(mcVersion, e.Since); ok && c < 0 {
			return "new"
		}
	}
	return ""
}

// defaultFor — значення за замовчуванням з урахуванням версії.
// У 26.3 Mojang змінила default для white-list з false на true
// (patch notes 26.3: «The white-list property is now true by default»).
func defaultFor(e PropEntry, mcVersion string) string {
	if e.Key == "white-list" {
		if c, ok := cmpMCVersion(mcVersion, "26.3"); ok && c >= 0 {
			return "true"
		}
	}
	return e.Default
}
