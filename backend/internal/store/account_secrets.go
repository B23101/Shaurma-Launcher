package store

// account_secrets.go — шифрування токенів акаунтів (Minecraft/Microsoft) у shaurma.json.
//
// Токени (AccessToken, MsAccessToken, RefreshToken, XblToken) і ключ CurseForge з налаштувань на диск пишуться у вигляді
// "enc1:<base64>" (Windows DPAPI / AES-GCM з ключем із HWID, див. shaurmaid.Sealer).
// У пам'яті (s.data.Accounts) вони завжди відкритим текстом — решта коду нічого не знає про шифрування.
//
// МІГРАЦІЯ. Старий shaurma.json має відкриті токени. При відкритті сховища вони
// читаються як є (гравцю НЕ треба перезаходити), після чого файл одразу
// перезаписується зашифрованим, а резервна копія .bak оновлюється, щоб відкриті
// токени не лишились у ній.
//
// ПОМИЛКИ. Якщо шифрування недоступне (немає HWID тощо) — токени пишуться як раніше:
// краще відкритий токен, ніж втрачений вхід. Якщо розшифрувати значення не вдалось
// (інший користувач Windows, інший ПК) — у пам'яті токен порожній (потрібен новий вхід
// лише для цього акаунта), а зашифрований запис зберігається й не перезаписується порожнім.

import (
	"encoding/json"
	"log"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/shaurmaid"
)

// newSealer — точка підміни в тестах.
var newSealer = func(dir string) *shaurmaid.Sealer {
	sl, err := shaurmaid.NewSealer(dir, shaurmaid.NewHWIDGenerator())
	if err != nil {
		log.Printf("store: шифрування токенів недоступне, токени лишаються як є: %v", err)
		return nil
	}
	return sl
}

// keptTokens — зашифровані значення, які не вдалось розшифрувати (див. ПОМИЛКИ).
type keptTokens struct{ access, msAccess, refresh, xbl string }

// initAccountSecrets розшифровує токени після читання з диска й мігрує відкриті.
func (s *Store) initAccountSecrets(dir string) {
	s.kept = map[string]keptTokens{}
	s.sealer = newSealer(dir)
	if s.sealer == nil {
		return
	}
	migrate := false
	for i := range s.data.Accounts {
		a := &s.data.Accounts[i]
		var k keptTokens
		for _, f := range []struct{ val, keep *string }{
			{&a.AccessToken, &k.access}, {&a.MsAccessToken, &k.msAccess}, {&a.RefreshToken, &k.refresh}, {&a.XblToken, &k.xbl},
		} {
			switch {
			case *f.val == "":
			case !shaurmaid.IsSealed(*f.val):
				migrate = true
			default:
				plain, err := s.sealer.Open(*f.val)
				if err != nil {
					log.Printf("store: токен акаунта %q не розшифровано (потрібен новий вхід): %v", a.Username, err)
					*f.keep, *f.val = *f.val, ""
				} else {
					*f.val = plain
				}
			}
		}
		if k != (keptTokens{}) {
			s.kept[a.ID] = k
		}
	}
	if s.openSettingsSecrets() {
		migrate = true
	}
	if migrate {
		if err := s.saveGlobal(); err != nil {
			log.Printf("store: не вдалось зашифрувати токени: %v", err)
			return
		}
		s.BackupNow() // перезаписує .bak вже зашифрованим файлом
	}
}

// sealField шифрує одне поле; порожнє значення повертає збережений зашифрований запис (якщо був).
func (s *Store) sealField(plain string, kept *string) string {
	if plain == "" {
		return *kept
	}
	*kept = "" // токен оновлено — старий нерозшифрований запис більше не потрібен
	sealed, err := s.sealer.Seal(plain)
	if err != nil {
		log.Printf("store: не вдалось зашифрувати токен: %v", err)
		return plain
	}
	return sealed
}

// accountsForDisk — копія акаунтів із зашифрованими токенами для запису у файл.
func (s *Store) accountsForDisk() []model.Account {
	if s.sealer == nil || len(s.data.Accounts) == 0 {
		return s.data.Accounts
	}
	out := make([]model.Account, len(s.data.Accounts))
	copy(out, s.data.Accounts)
	for i := range out {
		a := &out[i]
		k := s.kept[a.ID]
		a.AccessToken = s.sealField(a.AccessToken, &k.access)
		a.MsAccessToken = s.sealField(a.MsAccessToken, &k.msAccess)
		a.RefreshToken = s.sealField(a.RefreshToken, &k.refresh)
		a.XblToken = s.sealField(a.XblToken, &k.xbl)
		if k != (keptTokens{}) {
			s.kept[a.ID] = k
		} else {
			delete(s.kept, a.ID)
		}
	}
	return out
}

// settingsSecretKeys — ключі JSON налаштувань (s.data.Settings), чиї значення — секрети.
var settingsSecretKeys = []string{"curseForgeApiKey"}

// settingsMap розбирає налаштування у map; ok=false, якщо це не JSON-об'єкт (тоді нічого не чіпаємо).
func settingsMap(raw string) (map[string]json.RawMessage, bool) {
	if raw == "" {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil || m == nil {
		return nil, false
	}
	return m, true
}

// openSettingsSecrets розшифровує секретні ключі налаштувань у пам'яті; true — знайдено відкриті (потрібна міграція).
func (s *Store) openSettingsSecrets() (migrate bool) {
	s.keptSettings = map[string]string{}
	m, ok := settingsMap(s.data.Settings)
	if !ok {
		return false
	}
	changed := false
	for _, key := range settingsSecretKeys {
		var v string
		if raw, has := m[key]; !has || json.Unmarshal(raw, &v) != nil || v == "" {
			continue
		}
		if !shaurmaid.IsSealed(v) {
			migrate = true
			continue
		}
		plain, err := s.sealer.Open(v)
		if err != nil {
			log.Printf("store: секрет налаштувань %q не розшифровано: %v", key, err)
			s.keptSettings[key] = v
			plain = ""
		}
		b, _ := json.Marshal(plain)
		m[key] = b
		changed = true
	}
	if changed {
		if b, err := json.Marshal(m); err == nil {
			s.data.Settings = string(b)
		}
	}
	return migrate
}

// settingsForDisk — налаштування із зашифрованими секретними ключами для запису у файл.
func (s *Store) settingsForDisk() string {
	if s.sealer == nil {
		return s.data.Settings
	}
	m, ok := settingsMap(s.data.Settings)
	if !ok {
		return s.data.Settings
	}
	changed := false
	for _, key := range settingsSecretKeys {
		var v string
		raw, has := m[key]
		if !has || json.Unmarshal(raw, &v) != nil {
			continue
		}
		kept := s.keptSettings[key]
		out := s.sealField(v, &kept)
		if kept == "" {
			delete(s.keptSettings, key)
		} else {
			s.keptSettings[key] = kept
		}
		if out == v {
			continue
		}
		b, _ := json.Marshal(out)
		m[key] = b
		changed = true
	}
	if !changed {
		return s.data.Settings
	}
	b, err := json.Marshal(m)
	if err != nil {
		return s.data.Settings
	}
	return string(b)
}
