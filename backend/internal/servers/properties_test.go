package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Розділ 3.1: pre-1.14 watchdog fix ───────────────────────────────────
// Перенесено з properties.rs::tick_watchdog_disabled_only_below_1_14
// (рядки 143-159 оригіналу).

func TestIsPre114(t *testing.T) {
	trueCases := []string{"1.7.10", "1.8", "1.12.2", "1.13.2"}
	for _, v := range trueCases {
		if !isPre114(v) {
			t.Errorf("isPre114(%q) = false, очікували true", v)
		}
	}

	falseCases := []string{"1.14", "1.14.4", "1.20.1", "18w50a", "unknown"}
	for _, v := range falseCases {
		if isPre114(v) {
			t.Errorf("isPre114(%q) = true, очікували false", v)
		}
	}
}

func TestWriteDefaultServerProperties_TickWatchdogDisabledBelow114(t *testing.T) {
	dir := t.TempDir()
	if err := writeDefaultServerProperties(dir, "1.12.2"); err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatalf("читання: %v", err)
	}
	if !contains(string(data), "max-tick-time=-1") {
		t.Errorf("очікували max-tick-time=-1 для MC 1.12.2 (pre-1.14 watchdog fix), отримали:\n%s", data)
	}
}

func TestWriteDefaultServerProperties_TickWatchdogDefaultAt114AndAbove(t *testing.T) {
	dir := t.TempDir()
	if err := writeDefaultServerProperties(dir, "1.20.1"); err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatalf("читання: %v", err)
	}
	// Дефолтне зі схеми (60000), НЕ -1: watchdog-фікс не має застосовуватись
	// до версій ≥1.14.
	if contains(string(data), "max-tick-time=-1") {
		t.Errorf("watchdog-фікс застосовано до MC 1.20.1, не мав би:\n%s", data)
	}
	if !contains(string(data), "max-tick-time="+PropertiesSchema["max-tick-time"].Default) {
		t.Errorf("очікували дефолтне max-tick-time зі схеми для MC 1.20.1, отримали:\n%s", data)
	}
}

// ── Розділ 3.2: захист від newline-injection ────────────────────────────
// Перенесено з properties.rs, три тести:
// update_properties_rejects_newline_injection_in_an_existing_key,
// update_properties_rejects_newline_injection_in_a_new_key,
// update_properties_rejects_newline_in_the_key_itself (рядки 166-217).

func TestWriteServerProperties_RejectsNewlineInjectionInExistingKey(t *testing.T) {
	dir := t.TempDir()
	content := "#Minecraft server properties\nmotd=Hello\nonline-mode=true\n"
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	// Значення для існуючого ключа motd намагається інʼєктувати другий
	// рядок властивості через \r\n.
	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "motd", Value: "Hacked\r\nonline-mode=false"},
	})
	if err != nil {
		t.Fatalf("неочікувана помилка: %v (newline-injection має тихо відхилятись, не повертати помилку виклику)", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("читання: %v", err)
	}
	text := string(data)

	// Шкідливе значення не мало створити другий рядок online-mode.
	onlineModeLines := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "online-mode") {
			onlineModeLines++
		}
	}
	if onlineModeLines != 1 {
		t.Errorf("очікували рівно 1 рядок online-mode, знайдено %d:\n%s", onlineModeLines, text)
	}
	if contains(text, "online-mode=false") {
		t.Error("інʼєкція спрацювала: online-mode=false зʼявився у файлі")
	}

	// Весь шкідливий update відкидається повністю, не частково — motd
	// лишається попереднім значенням.
	if !contains(text, "motd=Hello") {
		t.Errorf("motd мав лишитись Hello (update відкинуто цілком), отримали:\n%s", text)
	}
	if contains(text, "Hacked") {
		t.Error("шкідливе значення \"Hacked\" потрапило у файл")
	}
}

func TestWriteServerProperties_RejectsNewlineInjectionInNewKey(t *testing.T) {
	dir := t.TempDir()
	content := "#Minecraft server properties\nmotd=Hello\n"
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "new-key", Value: "value\nonline-mode=false"},
	})
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("читання: %v", err)
	}
	text := string(data)

	if contains(text, "online-mode=false") {
		t.Error("інʼєкція через новий ключ спрацювала: online-mode=false зʼявився у файлі")
	}
	if contains(text, "new-key=") {
		t.Error("new-key не мав дописатись у файл — його значення містило newline")
	}
}

func TestWriteServerProperties_RejectsNewlineInTheKeyItself(t *testing.T) {
	dir := t.TempDir()
	content := "#Minecraft server properties\nmotd=Hello\n"
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "evil\nonline-mode", Value: "false"},
	})
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("читання: %v", err)
	}
	text := string(data)

	if contains(text, "online-mode=false") {
		t.Error("інʼєкція через сам ключ спрацювала: online-mode=false зʼявився у файлі")
	}
	if contains(text, "evil") {
		t.Error("шкідливий ключ \"evil\" потрапив у файл")
	}
}

// TestWriteServerProperties_NewlineInjectionDoesNotBlockOtherUpdates —
// перевіряє, що newline-injection (крок 2) НЕ all-or-nothing на відміну
// від ValidateProperty (крок 1, розділ 3.4): один шкідливий update
// відкидається, решта легітимних updates з того самого виклику
// застосовуються нормально.
func TestWriteServerProperties_NewlineInjectionDoesNotBlockOtherUpdates(t *testing.T) {
	dir := t.TempDir()
	content := "#Minecraft server properties\nmotd=Hello\nserver-port=25565\n"
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "motd", Value: "Hacked\r\nonline-mode=false"}, // відкидається
		{Key: "server-port", Value: "25577"},                // застосовується
	})
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	if !contains(text, "server-port=25577") {
		t.Errorf("легітимний update (server-port) не мав постраждати від відхилення сусіднього шкідливого update:\n%s", text)
	}
	if contains(text, "online-mode=false") {
		t.Error("шкідливий update не мав застосуватись")
	}
}

// TestWriteServerProperties_ValidateStillAllOrNothing — регресійний тест
// на пункт 3.4: ValidateProperty (тип/діапазон) лишається усе-або-нічого,
// на відміну від newline-injection перевірки вище. Невалідне числове
// значення для ОДНОГО ключа має відхилити ВЕСЬ виклик, нічого не
// записуючи — інша поведінка від newline-injection, і так і має бути.
func TestWriteServerProperties_ValidateStillAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	content := "#Minecraft server properties\nmotd=Hello\nserver-port=25565\n"
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "motd", Value: "New Motd"},   // саме по собі валідне
		{Key: "server-port", Value: "abc"}, // невалідне число
	})
	if err == nil {
		t.Fatal("очікували помилку через невалідний server-port")
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	if contains(text, "New Motd") {
		t.Error("усе-або-нічого порушено: valid update застосувався, попри невалідний сусідній update")
	}
	if !contains(text, "motd=Hello") {
		t.Errorf("файл мав лишитись незміненим при провалі ValidateProperty:\n%s", text)
	}
}

func TestParseProperties(t *testing.T) {
	dir := t.TempDir()
	content := `# Minecraft server properties
server-port=25566
gamemode=creative
online-mode=true
motd=Test Server
# Custom comment
view-distance=8
`
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	doc, err := ReadServerProperties(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}

	if len(doc.Entries) == 0 {
		t.Fatal("очікували хоча б один запис")
	}

	// Знаходимо server-port.
	found := false
	for _, e := range doc.Entries {
		if e.Key == "server-port" {
			if e.Value != "25566" {
				t.Errorf("очікували server-port=25566, отримали %s", e.Value)
			}
			if e.Type != "int" {
				t.Errorf("очікували type=int, отримали %s", e.Type)
			}
			found = true
		}
	}
	if !found {
		t.Error("server-port не знайдено")
	}

	// Перевіряємо збереження коментарів (через Raw).
	if doc.Raw == "" {
		t.Error("Raw має бути непорожнім")
	}
}

func TestValidateProperty_Int(t *testing.T) {
	tests := []struct {
		key, value string
		wantErr    bool
	}{
		{"server-port", "25565", false},
		{"server-port", "0", true},
		{"server-port", "70000", true},
		{"server-port", "abc", true},
		{"max-players", "20", false},
		{"max-players", "-1", true},
	}

	for _, tt := range tests {
		err := ValidateProperty(tt.key, tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateProperty(%q, %q) = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
		}
	}
}

func TestValidateProperty_Bool(t *testing.T) {
	tests := []struct {
		key, value string
		wantErr    bool
	}{
		{"online-mode", "true", false},
		{"online-mode", "false", false},
		{"online-mode", "yes", true},
		{"pvp", "true", false},
	}

	for _, tt := range tests {
		err := ValidateProperty(tt.key, tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateProperty(%q, %q) = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
		}
	}
}

func TestValidateProperty_Enum(t *testing.T) {
	tests := []struct {
		key, value string
		wantErr    bool
	}{
		{"gamemode", "survival", false},
		{"gamemode", "creative", false},
		{"gamemode", "invalid", true},
		{"difficulty", "normal", false},
		{"difficulty", "extreme", true},
	}

	for _, tt := range tests {
		err := ValidateProperty(tt.key, tt.value)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateProperty(%q, %q) = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
		}
	}
}

func TestWriteServerProperties_PreservesComments(t *testing.T) {
	dir := t.TempDir()
	content := `# Header comment
server-port=25565
# This is the gamemode
gamemode=survival
online-mode=true
`
	path := filepath.Join(dir, "server.properties")
	os.WriteFile(path, []byte(content), 0644)

	// Змінюємо тільки server-port.
	err := WriteServerProperties(dir, []PropUpdate{
		{Key: "server-port", Value: "25577"},
	})
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("читання: %v", err)
	}

	text := string(data)
	// Коментарі мають бути збережені.
	if !contains(text, "# Header comment") {
		t.Error("коментар # Header comment загублений")
	}
	if !contains(text, "# This is the gamemode") {
		t.Error("коментар # This is the gamemode загублений")
	}
	// Порт має бути оновлений.
	if !contains(text, "server-port=25577") {
		t.Errorf("port не оновлено: %s", text)
	}
	// Інші ключі не змінені.
	if !contains(text, "gamemode=survival") {
		t.Error("gamemode змінено")
	}
}

func TestJavaMajorForVersion(t *testing.T) {
	tests := []struct {
		mcVersion string
		want      int
	}{
		{"1.8.9", 8},
		{"1.12.2", 8},
		{"1.16.5", 8},
		{"1.17.1", 16},
		{"1.18.2", 17},
		{"1.19.4", 17},
		{"1.20.1", 17},
		{"1.20.4", 17},
		{"1.20.5", 21},
		{"1.21.1", 21},
		{"26.1", 25},
		{"26.2", 25},
	}

	for _, tt := range tests {
		got := JavaMajorForVersion(tt.mcVersion)
		if got != tt.want {
			t.Errorf("JavaMajorForVersion(%q) = %d, want %d", tt.mcVersion, got, tt.want)
		}
	}
}

func TestDefaultJVMArgs_Aikar(t *testing.T) {
	args := DefaultJVMArgs(17, 4096)
	if !contains(args, "-Xmx4096M") {
		t.Error("очікували -Xmx4096M")
	}
	if !contains(args, "-XX:+UseG1GC") {
		t.Error("очікували Aikar flags з G1GC")
	}
	if contains(args, "ZGC") {
		t.Error("не очікували ZGC для Java 17")
	}
}

func TestDefaultJVMArgs_ZGC(t *testing.T) {
	args := DefaultJVMArgs(25, 4096)
	if !contains(args, "ZGC") {
		t.Error("очікували ZGC для Java 25")
	}
	if contains(args, "G1GC") {
		t.Error("не очікували G1GC для Java 25")
	}
}

func TestBuildCommand_Fabric(t *testing.T) {
	srv := &Server{
		Core:      CoreFabric,
		Dir:       t.TempDir(),
		RAMMB:     2048,
		JavaMajor: 17,
	}
	os.WriteFile(filepath.Join(srv.Dir, "fabric-server-launch.jar"), []byte("fake"), 0644)

	args := buildCommand(srv)
	if len(args) == 0 {
		t.Fatal("очікували непорожні аргументи")
	}
	// Останній аргумент має бути nogui
	last := args[len(args)-1]
	if last != "nogui" {
		t.Errorf("очікували 'nogui' як останній аргумент, отримали %q", last)
	}
	// Повинен бути -jar <dir>/fabric-server-launch.jar
	found := false
	for i, a := range args {
		if strings.HasSuffix(a, "fabric-server-launch.jar") {
			if i > 0 && args[i-1] == "-jar" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("не знайдено '-jar fabric-server-launch.jar': %v", args)
	}
}

func TestBuildCommand_Velocity(t *testing.T) {
	srv := &Server{
		Core:      CoreVelocity,
		Dir:       t.TempDir(),
		RAMMB:     2048,
		JavaMajor: 21,
	}
	os.WriteFile(filepath.Join(srv.Dir, "velocity.jar"), []byte("fake"), 0644)

	args := buildCommand(srv)
	// Velocity НЕ має --nogui
	for _, a := range args {
		if a == "nogui" || a == "--nogui" {
			t.Error("Velocity не повинен мати --nogui")
		}
	}
	// Повинен бути -jar <dir>/velocity.jar
	found := false
	for i, a := range args {
		if strings.HasSuffix(a, "velocity.jar") {
			if i > 0 && args[i-1] == "-jar" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("не знайдено '-jar velocity.jar': %v", args)
	}
}

func TestBuildCommand_Forge120(t *testing.T) {
	dir := t.TempDir()
	// Forge ≥1.17 з args-файлом. ЗМІНЕНО (не логіка, сам тест): раніше тут
	// писався лише win_args.txt, що на Linux/macOS ніколи не знаходився
	// (findLoaderArgsFile/buildForgeArgs обидва обирають ім'я файлу за
	// runtime.GOOS, як і оригінал GDLauncher — platform_args_file_name()
	// у modloader_install.rs так само cfg!(windows)-залежний). Тест
	// проходив лише випадково — раніше буквально впав би у фолбек на
	// server.jar на не-Windows CI, і contains(a,"win_args.txt") ||
	// contains(a,"unix_args.txt") ніколи не спрацював би. GDLauncher-івський
	// тестовий хелпер write_neoforge_install (той самий тестовий модуль,
	// рядки 84-91 modloader_launch.rs) якраз пише ОБИДВА файли з цієї самої
	// причини — той самий підхід тут.
	forgeDir := filepath.Join(dir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10")
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(forgeDir, "win_args.txt"), []byte("--fml.forgeVersion=47.4.10"), 0644)
	os.WriteFile(filepath.Join(forgeDir, "unix_args.txt"), []byte("--fml.forgeVersion=47.4.10"), 0644)

	srv := &Server{
		Core:      CoreForge,
		Dir:       dir,
		RAMMB:     4096,
		JavaMajor: 17,
		LoaderVer: "1.20.1-47.4.10",
	}

	args := buildCommand(srv)
	// Повинен використовувати @libraries/.../win_args.txt
	found := false
	for _, a := range args {
		if contains(a, "win_args.txt") || contains(a, "unix_args.txt") {
			found = true
		}
	}
	if !found {
		t.Errorf("не знайдено @libraries args для Forge ≥1.17: %v", args)
	}
}

func TestScanFreePort(t *testing.T) {
	port := ScanFreePort(25565, nil, "", nil)
	if port < 25565 || port > 65535 {
		t.Errorf("очікували вільний порт 25565-65535, отримали %d", port)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
