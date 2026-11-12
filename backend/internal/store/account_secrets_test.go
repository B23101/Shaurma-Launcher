package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/shaurmaid"
)

type fakeHWID struct{}

func (fakeHWID) HWID() (string, error) { return strings.Repeat("a", 64), nil }

func useTestSealer(t *testing.T) {
	t.Helper()
	old := newSealer
	newSealer = func(dir string) *shaurmaid.Sealer {
		sl, err := shaurmaid.NewSealer(dir, fakeHWID{})
		if err != nil {
			t.Fatal(err)
		}
		return sl
	}
	t.Cleanup(func() { newSealer = old })
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if b, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && strings.HasPrefix(e.Name(), "shaurma.json") {
			sb.Write(b)
		}
	}
	return sb.String()
}

// Старий файл з відкритими токенами: вхід зберігається, на диску й у .bak токенів у відкритому вигляді не лишається.
func TestAccountTokensMigrate(t *testing.T) {
	useTestSealer(t)
	dir := t.TempDir()
	old := `{"accounts":[{"id":"1","username":"Steve","type":"microsoft","accessToken":"AT-secret","msAccessToken":"MS-secret","refreshToken":"RT-secret","xblToken":"XBL-secret"}],"settings":"{\"theme\":\"dark\",\"curseForgeApiKey\":\"CF-secret\"}"}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName+BackupSuffix), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, filepath.Join(dir, "inst"))
	if err != nil {
		t.Fatal(err)
	}
	accs, _ := s.LoadAccounts()
	if len(accs) != 1 || accs[0].AccessToken != "AT-secret" || accs[0].MsAccessToken != "MS-secret" || accs[0].RefreshToken != "RT-secret" {
		t.Fatalf("токени мають лишитись доступними в пам'яті: %+v", accs)
	}
	if raw, _, _ := s.GetSettingsRaw(); !strings.Contains(raw, "CF-secret") || !strings.Contains(raw, `"theme":"dark"`) || accs[0].XblToken != "XBL-secret" {
		t.Fatalf("секрети мають лишитись доступними в пам'яті: %q %+v", raw, accs[0])
	}
	disk := readAll(t, dir)
	for _, secret := range []string{"AT-secret", "MS-secret", "RT-secret", "XBL-secret", "CF-secret"} {
		if strings.Contains(disk, secret) {
			t.Fatalf("відкритий токен %s лишився на диску (файл або .bak)", secret)
		}
	}
	if !strings.Contains(disk, "enc1:") {
		t.Fatal("очікувались зашифровані токени")
	}
	// Повторне відкриття читає зашифроване.
	s2, err := Open(dir, filepath.Join(dir, "inst"))
	if err != nil {
		t.Fatal(err)
	}
	accs, _ = s2.LoadAccounts()
	if accs[0].RefreshToken != "RT-secret" {
		t.Fatalf("після перезапуску токен втрачено: %+v", accs[0])
	}
	if raw, _, _ := s2.GetSettingsRaw(); !strings.Contains(raw, "CF-secret") {
		t.Fatalf("після перезапуску ключ CurseForge втрачено: %q", raw)
	}
	// Збереження налаштувань із новим ключем теж шифрується.
	if err := s2.SaveSettingsRaw(`{"curseForgeApiKey":"CF-new"}`); err != nil {
		t.Fatal(err)
	}
	if d := readAll(t, dir); strings.Contains(d, "CF-new") {
		t.Fatal("новий ключ CurseForge записано відкрито")
	}
	// Зміна токена зберігається зашифрованою.
	accs[0].AccessToken = "AT-new"
	if err := s2.SaveAccounts(accs); err != nil {
		t.Fatal(err)
	}
	if d := readAll(t, dir); strings.Contains(d, "AT-new") {
		t.Fatal("новий токен записано відкрито")
	}
	s3, _ := Open(dir, filepath.Join(dir, "inst"))
	accs, _ = s3.LoadAccounts()
	if accs[0].AccessToken != "AT-new" {
		t.Fatalf("новий токен не прочитався: %+v", accs[0])
	}
}

// Не розшифрувалось (інший ключ) — токен порожній у пам'яті, але зашифрований запис не губиться при збереженні.
func TestAccountTokensUndecryptableKept(t *testing.T) {
	useTestSealer(t)
	dir := t.TempDir()
	s, _ := Open(dir, filepath.Join(dir, "inst"))
	if err := s.SaveAccounts([]model.Account{{ID: "1", Username: "A", RefreshToken: "RT-1"}}); err != nil {
		t.Fatal(err)
	}
	// «Інший ПК»: інша сіль → інший ключ.
	_ = os.Remove(filepath.Join(dir, ".salt"))
	s2, _ := Open(dir, filepath.Join(dir, "inst"))
	accs, _ := s2.LoadAccounts()
	if accs[0].RefreshToken != "" {
		t.Fatalf("очікувався порожній токен, є %q", accs[0].RefreshToken)
	}
	before := readAll(t, dir)
	accs[0].Username = "B" // збереження інших змін не повинно стирати шифротекст
	if err := s2.SaveAccounts(accs); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAll(t, dir), "enc1:") || !strings.Contains(before, "enc1:") {
		t.Fatal("зашифрований запис мав зберегтись")
	}
}

// Шифрування недоступне — токени пишуться як раніше, вхід не втрачається.
func TestAccountTokensNoSealer(t *testing.T) {
	old := newSealer
	newSealer = func(string) *shaurmaid.Sealer { return nil }
	defer func() { newSealer = old }()
	dir := t.TempDir()
	s, _ := Open(dir, filepath.Join(dir, "inst"))
	if err := s.SaveAccounts([]model.Account{{ID: "1", RefreshToken: "RT-1"}}); err != nil {
		t.Fatal(err)
	}
	s2, _ := Open(dir, filepath.Join(dir, "inst"))
	accs, _ := s2.LoadAccounts()
	if accs[0].RefreshToken != "RT-1" {
		t.Fatalf("%+v", accs[0])
	}
}
