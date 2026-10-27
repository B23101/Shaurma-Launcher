package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/model"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir(), "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSettingsRoundtrip(t *testing.T) {
	s := openTest(t)
	if _, ok, err := s.GetSettingsRaw(); err != nil || ok {
		t.Fatalf("порожні дані: ok=%v err=%v, want ok=false", ok, err)
	}
	const raw = `{"language":"en","autoUpdate":false,"updateChannel":"beta"}`
	if err := s.SaveSettingsRaw(raw); err != nil {
		t.Fatalf("SaveSettingsRaw: %v", err)
	}
	got, ok, err := s.GetSettingsRaw()
	if err != nil || !ok || got != raw {
		t.Fatalf("GetSettingsRaw = %q, ok=%v, err=%v; want %q", got, ok, err, raw)
	}
	if err := s.SaveSettingsRaw(`{"language":"uk"}`); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, _, _ = s.GetSettingsRaw()
	if got != `{"language":"uk"}` {
		t.Fatalf("після upsert: %q", got)
	}
}

func TestAccountsRoundtrip(t *testing.T) {
	s := openTest(t)
	accounts := []model.Account{
		{ID: "a1", Username: "First", Type: "ms", AccessToken: "tok1", UUID: "u1", IsLicensed: true},
		{ID: "a2", Username: "Second", RefreshToken: "rt2"},
	}
	if err := s.SaveAccounts(accounts); err != nil {
		t.Fatalf("SaveAccounts: %v", err)
	}
	got, err := s.LoadAccounts()
	if err != nil {
		t.Fatalf("LoadAccounts: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a1" || !got[0].IsLicensed || got[0].AccessToken != "tok1" {
		t.Fatalf("перший акаунт = %+v", got[0])
	}
	if got[1].ID != "a2" || got[1].IsLicensed {
		t.Fatalf("другий акаунт = %+v", got[1])
	}
	if err := s.SaveAccounts([]model.Account{{ID: "b1", Username: "New"}}); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	got, _ = s.LoadAccounts()
	if len(got) != 1 || got[0].ID != "b1" {
		t.Fatalf("після rewrite: %+v", got)
	}
}

func TestPlaytimeIncrement(t *testing.T) {
	s := openTest(t)
	if err := s.AddPlaytime("pack1", 10); err != nil {
		t.Fatalf("AddPlaytime: %v", err)
	}
	if err := s.AddPlaytime("pack1", 5); err != nil {
		t.Fatalf("AddPlaytime: %v", err)
	}
	if err := s.AddPlaytime("pack2", 7); err != nil {
		t.Fatalf("AddPlaytime: %v", err)
	}
	pt, err := s.LoadPlaytime()
	if err != nil {
		t.Fatalf("LoadPlaytime: %v", err)
	}
	if pt["pack1"] != 15 || pt["pack2"] != 7 {
		t.Fatalf("playtime = %+v, want pack1=15 pack2=7", pt)
	}
}

func TestInstanceConfigPerFile(t *testing.T) {
	dir := t.TempDir()
	instancesDir := filepath.Join(dir, "instances")
	s, err := Open(dir, instancesDir)
	if err != nil {
		t.Fatal(err)
	}

	cfg := model.InstanceConfig{ID: "p1", MaxRAMOverride: 2048, UseCustomCommands: true, PinnedServerIPs: []string{"1.1.1.1"}}
	if err := s.SaveInstanceConfig(cfg); err != nil {
		t.Fatalf("SaveInstanceConfig: %v", err)
	}

	// Перевіряємо що instance.json створився
	instFile := filepath.Join(instancesDir, "p1", "instance.json")
	if _, err := os.Stat(instFile); err != nil {
		t.Fatalf("instance.json не створено: %v", err)
	}

	got, ok, err := s.GetInstanceConfig("p1")
	if err != nil || !ok {
		t.Fatalf("GetInstanceConfig ok=%v err=%v", ok, err)
	}
	if got.MaxRAMOverride != 2048 || !got.UseCustomCommands || len(got.PinnedServerIPs) != 1 {
		t.Fatalf("confіг = %+v", got)
	}

	if _, ok, _ := s.GetInstanceConfig("nope"); ok {
		t.Fatalf("невідома збірка знайдена")
	}

	all, err := s.LoadInstanceConfigs()
	if err != nil || len(all) != 1 {
		t.Fatalf("LoadInstanceConfigs = %+v, err=%v", all, err)
	}

	if err := s.DeleteInstanceConfig("p1"); err != nil {
		t.Fatalf("DeleteInstanceConfig: %v", err)
	}
	if _, err := os.Stat(instFile); !os.IsNotExist(err) {
		t.Fatalf("instance.json не видалено")
	}
}

func TestMinecraftDefaultsRoundtrip(t *testing.T) {
	s := openTest(t)
	if _, ok, err := s.GetMinecraftDefaultsRaw(); err != nil || ok {
		t.Fatalf("порожні дані: ok=%v err=%v, want ok=false", ok, err)
	}
	const raw = `{"enabled":true,"language":"uk_ua","guiScale":3}`
	if err := s.SaveMinecraftDefaultsRaw(raw); err != nil {
		t.Fatalf("SaveMinecraftDefaultsRaw: %v", err)
	}
	got, ok, err := s.GetMinecraftDefaultsRaw()
	if err != nil || !ok || got != raw {
		t.Fatalf("GetMinecraftDefaultsRaw = %q, ok=%v, err=%v; want %q", got, ok, err, raw)
	}
}

func TestSetupProgressRoundtrip(t *testing.T) {
	s := openTest(t)
	if _, ok, err := s.LoadSetupProgress(); err != nil || ok {
		t.Fatalf("порожня: ok=%v err=%v", ok, err)
	}
	p := model.SetupProgress{Step: 2, InstanceDir: "C:\\inst", MaxRAM: 4096}
	if err := s.SaveSetupProgress(p); err != nil {
		t.Fatalf("SaveSetupProgress: %v", err)
	}
	got, ok, err := s.LoadSetupProgress()
	if err != nil || !ok {
		t.Fatalf("LoadSetupProgress ok=%v err=%v", ok, err)
	}
	if got.Step != 2 || got.MaxRAM != 4096 {
		t.Fatalf("progress = %+v", got)
	}
	if err := s.DeleteSetupProgress(); err != nil {
		t.Fatalf("DeleteSetupProgress: %v", err)
	}
	if _, ok, _ := s.LoadSetupProgress(); ok {
		t.Fatalf("чернетку не видалено")
	}
}

func TestJSONPersistenceAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	instancesDir := filepath.Join(dir, "instances")

	s1, err := Open(dir, instancesDir)
	if err != nil {
		t.Fatal(err)
	}
	s1.SaveSettingsRaw(`{"language":"en"}`)
	s1.SaveAccounts([]model.Account{{ID: "a1", Username: "Test"}})
	s1.SaveInstanceConfig(model.InstanceConfig{ID: "p1", MaxRAMOverride: 4096})
	s1.AddPlaytime("p1", 100)
	s1.Close()

	s2, err := Open(dir, instancesDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	raw, ok, _ := s2.GetSettingsRaw()
	if !ok || raw != `{"language":"en"}` {
		t.Fatalf("settings не збереглись: %q, ok=%v", raw, ok)
	}

	acc, _ := s2.LoadAccounts()
	if len(acc) != 1 || acc[0].ID != "a1" {
		t.Fatalf("акаунти не збереглись: %+v", acc)
	}

	cfg, ok, _ := s2.GetInstanceConfig("p1")
	if !ok || cfg.MaxRAMOverride != 4096 {
		t.Fatalf("instance config не зберігся: %+v", cfg)
	}

	pt, _ := s2.LoadPlaytime()
	if pt["p1"] != 100 {
		t.Fatalf("playtime не зберігся: %+v", pt)
	}
}

func TestScanAndRegisterInstances(t *testing.T) {
	dir := t.TempDir()
	instancesDir := filepath.Join(dir, "instances")
	os.MkdirAll(filepath.Join(instancesDir, "myPack", ".minecraft"), 0755)
	os.MkdirAll(filepath.Join(instancesDir, "justAFolder"), 0755) // без .minecraft
	os.MkdirAll(filepath.Join(instancesDir, ".hidden"), 0755)

	s, err := Open(dir, instancesDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	n := s.ScanAndRegisterInstances()
	if n != 1 {
		t.Fatalf("ScanAndRegisterInstances = %d, want 1 (лише myPack з .minecraft)", n)
	}

	// Перевіряємо що instance.json створився для myPack
	instJSONPath := filepath.Join(instancesDir, "myPack", "instance.json")
	if _, err := os.Stat(instJSONPath); err != nil {
		t.Fatalf("instance.json не створено для myPack")
	}

	// Перевіряємо що instance.json має дефолти
	raw, err := os.ReadFile(instJSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var icfg model.InstanceConfig
	if err := json.Unmarshal(raw, &icfg); err != nil {
		t.Fatal(err)
	}
	if icfg.Loader != "vanilla" {
		t.Errorf("Loader = %q, want vanilla", icfg.Loader)
	}
	if icfg.Name != "myPack" {
		t.Errorf("Name = %q, want myPack", icfg.Name)
	}

	// Перевіряємо registry
	reg, _ := s.LoadBuildsRegistry()
	if len(reg) != 1 || reg[0].ID != "myPack" {
		t.Fatalf("registry = %+v", reg)
	}

	// Повторний скан — не дублює
	n2 := s.ScanAndRegisterInstances()
	if n2 != 0 {
		t.Fatalf("другий скан = %d, want 0", n2)
	}
}

func TestNoBackupForEmptyData(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	// Не пишемо нічого — бекапу не має бути
	s.BackupNow()

	bakPath := dir + string(os.PathSeparator) + FileName + BackupSuffix
	if _, err := os.Stat(bakPath); err == nil {
		t.Errorf("backup створено для порожніх даних — не треба")
	}
}

func TestBackupOnlyForRealData(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	// Пишемо реальні дані
	s.SaveSettingsRaw(`{"language":"uk"}`)
	s.BackupNow()

	bakPath := dir + string(os.PathSeparator) + FileName + BackupSuffix
	if _, err := os.Stat(bakPath); err != nil {
		t.Errorf("backup не створено для реальних даних")
	}
}

// ── Інтеграційні тести: повний шлях міграції ───────────────────────

func TestFullRoundtrip(t *testing.T) {
	// 1. Відкриваємо, пишемо ВСІ типи даних
	dir := t.TempDir()
	instDir := filepath.Join(dir, "instances")
	s, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}

	// Settings
	settingsRaw := `{"language":"uk","accent":"orange","instanceDir":"` + instDir + `"}`
	if err := s.SaveSettingsRaw(settingsRaw); err != nil {
		t.Fatal(err)
	}

	// Accounts
	accs := []model.Account{{ID: "acc1", Username: "Bogdan", Type: "microsoft", AccessToken: "tok", ExpiresAt: 1234567890, UUID: "uuid1"}}
	if err := s.SaveAccounts(accs); err != nil {
		t.Fatal(err)
	}

	// Playtime (per-instance)
	if err := s.AddPlaytime("pack1", 3600); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlaytime("pack1", 1800); err != nil {
		t.Fatal(err)
	}

	// Playtime sessions
	if err := s.AddPlaytimeSession("pack1", "acc1", "Bogdan", 600, time.Now()); err != nil {
		t.Fatal(err)
	}

	// Instance config (per-instance)
	icfg := model.InstanceConfig{ID: "pack1", Name: "MyPack", MCVersion: "1.20.1", Loader: "fabric", LoaderVersion: "0.15.11", Color: "#ff0000", Source: "modrinth", PlaytimeSeconds: 5400, MaxRAMOverride: 4096}
	if err := s.SaveInstanceConfig(icfg); err != nil {
		t.Fatal(err)
	}

	// Builds registry
	reg := []RegistryEntry{{ID: "pack1", Kind: "custom", InstalledVersion: "v1", InstalledAt: time.Now().Unix()}}
	if err := s.SaveBuildsRegistry(reg); err != nil {
		t.Fatal(err)
	}

	// Custom packs → тепер в custom-packs.json, не в globalData
	// (тест пропускаємо — custom packs не зберігаються в shaurma.json)

	// Gallery meta
	gallery := []GalleryMetaEntry{{PackID: "pack1", FileName: "screenshot.png", Data: `"base64data"`}}
	if err := s.SaveGalleryMeta(gallery); err != nil {
		t.Fatal(err)
	}

	// Shortcuts
	shortcuts := map[string]string{"pack1": "MyPack.lnk"}
	if err := s.SaveShortcuts(shortcuts); err != nil {
		t.Fatal(err)
	}

	// Wardrobe presets (by username)
	if err := s.SaveWardrobePresets("Bogdan", `{"skin":"url"}`); err != nil {
		t.Fatal(err)
	}

	// Install state — тепер в install-state/, не в globalData
	// (тест пропускаємо)

	// Export settings
	if err := s.SaveExportSettings("pack1", `{"format":"zip"}`); err != nil {
		t.Fatal(err)
	}

	// Duplicate settings
	if err := s.SaveDuplicateSettings("pack1", `{"keepSaves":true}`); err != nil {
		t.Fatal(err)
	}

	// Meta
	if err := s.SetMeta("flag1", "done"); err != nil {
		t.Fatal(err)
	}

	// Backup + закриваємо
	s.BackupNow()
	s.Close()

	// 2. Перевіряємо що .db немає (не створювався)
	dbPath := filepath.Join(dir, "shaurma.db")
	if _, err := os.Stat(dbPath); err == nil {
		t.Error("shaurma.db не мав існувати — ми на JSON")
	}

	// 3. Відкриваємо заново
	s2, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// 4. Перевіряємо ВСІ дані

	// Settings
	raw, ok, err := s2.GetSettingsRaw()
	if err != nil || !ok {
		t.Fatal("settings не знайдено")
	}
	if len(raw) < 5 {
		t.Errorf("settings занадто короткий: %q", raw)
	}

	// Accounts
	accs2, err := s2.LoadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs2) != 1 || accs2[0].Username != "Bogdan" {
		t.Errorf("accounts = %+v", accs2)
	}

	// Playtime
	pt, err := s2.LoadPlaytime()
	if err != nil {
		t.Fatal(err)
	}
	if pt["pack1"] != 5400 {
		t.Errorf("playtime[pack1] = %d, want 5400", pt["pack1"])
	}

	// Playtime sessions
	sess, err := s2.LoadPlaytimeSessions("Bogdan")
	if err != nil {
		t.Fatal(err)
	}
	if len(sess) != 1 || sess[0].InstanceID != "pack1" {
		t.Errorf("sessions = %+v", sess)
	}

	// Instance config
	icfg2, ok, err := s2.GetInstanceConfig("pack1")
	if err != nil || !ok {
		t.Fatal("instance config не знайдено")
	}
	if icfg2.MCVersion != "1.20.1" || icfg2.Loader != "fabric" || icfg2.MaxRAMOverride != 4096 {
		t.Errorf("instance config = %+v", icfg2)
	}

	// Builds registry
	reg2, err := s2.LoadBuildsRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg2) != 1 || reg2[0].ID != "pack1" {
		t.Errorf("registry = %+v", reg2)
	}

	// Custom packs — тепер в custom-packs.json, не в globalData
	// (перевірка пропущена)

	// Gallery
	gal, err := s2.LoadGalleryMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(gal) != 1 || gal[0].PackID != "pack1" {
		t.Errorf("gallery = %+v", gal)
	}

	// Shortcuts
	sc, err := s2.LoadShortcuts()
	if err != nil {
		t.Fatal(err)
	}
	if sc["pack1"] != "MyPack.lnk" {
		t.Errorf("shortcuts = %+v", sc)
	}

	// Wardrobe presets (by username!)
	wp, ok, err := s2.LoadWardrobePresets("Bogdan")
	if err != nil || !ok {
		t.Fatal("wardrobe presets не знайдено")
	}
	if !json.Valid([]byte(wp)) {
		t.Error("wardrobe presets не валідний JSON")
	}
	// Тестуємо що за старим accountID не знайде
	wp2, ok2, _ := s2.LoadWardrobePresets("acc1")
	if ok2 || wp2 != "" {
		t.Error("wardrobe presets знайдено за accountID — має бути тільки за username")
	}

	// Install state — тепер в install-state/, не в globalData
	// (перевірка пропущена)

	// Export settings
	es, ok, err := s2.LoadExportSettings("pack1")
	if err != nil || !ok {
		t.Fatal("export settings не знайдено")
	}
	if !json.Valid([]byte(es)) {
		t.Error("export settings не валідний JSON")
	}

	// Duplicate settings
	ds, ok, err := s2.LoadDuplicateSettings("pack1")
	if err != nil || !ok {
		t.Fatal("duplicate settings не знайдено")
	}
	if !json.Valid([]byte(ds)) {
		t.Error("duplicate settings не валідний JSON")
	}

	// Meta
	v, ok, err := s2.GetMeta("flag1")
	if err != nil || !ok || v != "done" {
		t.Errorf("meta flag1 = %q ok=%v err=%v", v, ok, err)
	}
}

func TestMigrateCustomPacksToInstance(t *testing.T) {
	dir := t.TempDir()
	instDir := filepath.Join(dir, "instances")
	s, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Пишемо custom packs в custom-packs.json
	cpPath := filepath.Join(dir, "custom-packs.json")
	cpData := []byte(`[{"id":"zombiecraft","name":"ZombieCraft","description":"Zombie pack","mcVersion":"1.20.1","loader":"fabric","loaderVersion":"0.15.11","color":"#ff0000","source":"curseforge"}]`)
	_ = os.WriteFile(cpPath, cpData, 0644)

	// Викликаємо міграцію
	s.MigrateCustomPacksToInstance()

	// Перевіряємо що instance.json створився з правильними даними
	instPath := filepath.Join(instDir, "zombiecraft", "instance.json")
	raw, err := os.ReadFile(instPath)
	if err != nil {
		t.Fatalf("instance.json не створено: %v", err)
	}
	var icfg model.InstanceConfig
	if err := json.Unmarshal(raw, &icfg); err != nil {
		t.Fatal(err)
	}
	if icfg.Name != "ZombieCraft" {
		t.Errorf("Name = %q, want ZombieCraft", icfg.Name)
	}
	if icfg.MCVersion != "1.20.1" {
		t.Errorf("MCVersion = %q, want 1.20.1", icfg.MCVersion)
	}
	if icfg.Loader != "fabric" {
		t.Errorf("Loader = %q, want fabric", icfg.Loader)
	}
	if icfg.LoaderVersion != "0.15.11" {
		t.Errorf("LoaderVersion = %q, want 0.15.11", icfg.LoaderVersion)
	}
	if icfg.Color != "#ff0000" {
		t.Errorf("Color = %q, want #ff0000", icfg.Color)
	}
	if icfg.Source != "curseforge" {
		t.Errorf("Source = %q, want curseforge", icfg.Source)
	}

	// Перевіряємо що custom-packs.json лишився (metadata перенесена в instance.json)
	if _, err := os.Stat(cpPath); err != nil {
		t.Errorf("custom-packs.json має існувати: %v", err)
	}
}

func TestScanRegistersVanillaForInstanceJSONOnly(t *testing.T) {
	// Папка з instance.json але БЕЗ .minecraft — все одно реєструється
	dir := t.TempDir()
	instDir := filepath.Join(dir, "instances")
	packDir := filepath.Join(instDir, "onlyJSON")
	os.MkdirAll(packDir, 0755)
	icfg := model.InstanceConfig{ID: "onlyJSON", Name: "Only JSON"}
	data, _ := json.MarshalIndent(icfg, "", "  ")
	os.WriteFile(filepath.Join(packDir, "instance.json"), data, 0644)

	s, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	n := s.ScanAndRegisterInstances()
	if n != 1 {
		t.Fatalf("ScanAndRegisterInstances = %d, want 1", n)
	}

	// Перевіряємо що loader дефолтнув на vanilla
	raw, _ := os.ReadFile(filepath.Join(packDir, "instance.json"))
	var cfg model.InstanceConfig
	json.Unmarshal(raw, &cfg)
	if cfg.Loader != "vanilla" {
		t.Errorf("Loader = %q, want vanilla", cfg.Loader)
	}
}

func TestScanDoesNotOverwriteExistingFields(t *testing.T) {
	dir := t.TempDir()
	instDir := filepath.Join(dir, "instances")
	packDir := filepath.Join(instDir, "myPack")
	os.MkdirAll(packDir, 0755)

	// Існуючий instance.json з вже заданими полями
	icfg := model.InstanceConfig{ID: "myPack", Name: "Збережена назва", MCVersion: "1.21", Loader: "forge", Color: "#00ff00"}
	data, _ := json.MarshalIndent(icfg, "", "  ")
	os.WriteFile(filepath.Join(packDir, "instance.json"), data, 0644)

	s, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	n := s.ScanAndRegisterInstances()
	if n != 1 {
		t.Fatalf("ScanAndRegisterInstances = %d, want 1", n)
	}

	// Перевіряємо що нічого НЕ перезаписано
	raw, _ := os.ReadFile(filepath.Join(packDir, "instance.json"))
	var cfg model.InstanceConfig
	json.Unmarshal(raw, &cfg)
	if cfg.Name != "Збережена назва" {
		t.Errorf("Name перезаписано: %q", cfg.Name)
	}
	if cfg.Loader != "forge" {
		t.Errorf("Loader перезаписано: %q", cfg.Loader)
	}
	if cfg.MCVersion != "1.21" {
		t.Errorf("MCVersion перезаписано: %q", cfg.MCVersion)
	}
}

func TestDeleteDBFiles(t *testing.T) {
	dir := t.TempDir()
	instDir := filepath.Join(dir, "instances")
	s, err := Open(dir, instDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Створюємо фейкові DB файли
	dbPath := filepath.Join(dir, "shaurma.db")
	os.WriteFile(dbPath, []byte("fake"), 0644)
	os.WriteFile(dbPath+"-wal", []byte("fake"), 0644)
	os.WriteFile(dbPath+"-shm", []byte("fake"), 0644)

	// Видаляємо
	s.removeDBFiles(dbPath)

	// Перевіряємо
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
			t.Errorf("файл %s не видалено", suffix)
		}
	}
}
