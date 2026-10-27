package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// deviceIDFileName — СТАРИЙ шлях сі́лі DeviceID (до переносу в
// shaurma.json). Лишається лише для одноразової міграції даних
// існуючих користувачів (loadOrCreateSalt читає його, якщо ключа в
// shaurma.json ще нема) — новий код цей файл більше не пише.
const deviceIDFileName = "device-id.txt"

// deviceIDMetaKey — ключ солі DeviceID у shaurma.json (Store.GetMeta/
// SetMeta). РЕГРЕСІЯ (виправлено): раніше сіль жила в окремому
// device-id.txt поруч із settings.json — усупереч задокументованій
// архітектурі («жодних дрібних файлів-міток поза shaurma.json»).
const deviceIDMetaKey = "deviceIdSalt"

// legacyDeviceIDPath — шлях до старого файла сі́лі (для міграції).
func (m *Manager) legacyDeviceIDPath() string { return filepath.Join(m.ConfigDir(), deviceIDFileName) }

// DeviceID повертає стабільний ідентифікатор ЦЬОГО фізичного пристрою для
// підрахунку унікальних встановлень лаунчера на сервері (/install, worker.js).
//
// Навіщо не просто random UUID у settings.json:
//   - видалення налаштувань / переустановлення лаунчера НЕ повинно рахуватись
//     як нове встановлення для того самого користувача.
//
// Навіщо не просто "голий" apparatus fingerprint (MachineGUID тощо):
//   - апаратний ідентифікатор сам по собі — це вже трекінг заліза без згоди
//     користувача поза межами лаунчера; хочемо ID, який НЕ можна зіставити
//     з ідентифікатором пристрою в інших застосунках.
//
// Тому DeviceID = sha256(machineSeed + persistedRandomSalt):
//   - machineSeed — стабільний для цього заліза (MachineGuid на Windows,
//     /etc/machine-id на Linux, IOPlatformUUID на macOS; best-effort, з
//     фолбеком на порожній рядок якщо недоступний);
//   - persistedRandomSalt — 32 випадкові байти, згенеровані ОДИН РАЗ і
//     записані у shaurma.json (Store.GetMeta/SetMeta, ключ
//     deviceIDMetaKey). Захищає від того, щоб сирий апаратний
//     ідентифікатор коли-небудь пішов на сервер чи використовувався як є.
//
// Результат: перевстановлення лаунчера на тому ж ПК (сіль лишається на
// диску) → той самий DeviceID → сервер бачить дублікат і не рахує вдруге.
// Повне видалення лаунчера І Windows/диска (сіль втрачена) на тій самій
// фізичній машині дасть новий DeviceID — прийнятний компроміс: неможливо
// зробити систему і стійкою до повного знищення слідів, і такою, що не
// зберігає жодних даних локально.
func (m *Manager) DeviceID() (string, error) {
	salt, err := m.loadOrCreateSalt()
	if err != nil {
		return "", err
	}
	seed := machineSeed()
	sum := sha256.Sum256(append(append([]byte{}, seed...), salt...))
	return hex.EncodeToString(sum[:]), nil
}

// loadOrCreateSalt читає сіль з shaurma.json або генерує нову й зберігає
// туди. Якщо ключа в shaurma.json ще нема, але лишився СТАРИЙ
// device-id.txt (юзер оновився зі старої версії лаунчера) — переносить
// його вміст замість генерації нової солі: інакше кожен існуючий
// користувач при першому старті нової версії отримав би НОВИЙ DeviceID,
// і сервер порахував би його як «нове встановлення» — саме той подвійний
// залік, від якого цей механізм і мав захищати.
func (m *Manager) loadOrCreateSalt() ([]byte, error) {
	if m.st != nil {
		if hexSalt, ok, err := m.st.GetMeta(deviceIDMetaKey); err == nil && ok {
			if decoded, derr := hex.DecodeString(strings.TrimSpace(hexSalt)); derr == nil && len(decoded) == 32 {
				return decoded, nil
			}
			// Пошкоджене значення — перегенеровуємо нижче.
		}

		// Міграція зі старого device-id.txt (одноразово): файл читаємо, але
		// більше НЕ пишемо — джерелом правди стає shaurma.json.
		if data, err := os.ReadFile(m.legacyDeviceIDPath()); err == nil {
			s := strings.TrimSpace(string(data))
			if decoded, derr := hex.DecodeString(s); derr == nil && len(decoded) == 32 {
				if serr := m.st.SetMeta(deviceIDMetaKey, s); serr == nil {
					_ = os.Remove(m.legacyDeviceIDPath())
				}
				return decoded, nil
			}
		}

		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		if err := m.st.SetMeta(deviceIDMetaKey, hex.EncodeToString(salt)); err != nil {
			return nil, err
		}
		return salt, nil
	}

	// Сховище недоступне (dev/тести без ініціалізації) — фолбек на старий
	// файловий шлях, щоб DeviceID() не падав у середовищах без Store.
	return m.loadOrCreateSaltLegacyFile()
}

func (m *Manager) loadOrCreateSaltLegacyFile() ([]byte, error) {
	path := m.legacyDeviceIDPath()
	if data, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(data))
		if decoded, derr := hex.DecodeString(s); derr == nil && len(decoded) == 32 {
			return decoded, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(salt)), 0600); err != nil {
		return nil, err
	}
	return salt, nil
}
