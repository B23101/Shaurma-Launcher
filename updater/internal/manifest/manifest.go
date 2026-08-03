// Package manifest описує launcher-version.json — контракт між
// shrm-updater і CDN (worker.js, шлях launcher/launcher-version.json).
//
// Формат навмисно мінімальний: раніше (JavaFX-ера) лаунчер складався з
// купи файлів (jar, kілька exe, css) і манiфест описував масив
// компонентів. Тепер увесь лаунчер — ОДИН файл launcher.exe (Wails,
// embed frontend), тож компонент рівно один. Масив лишили (не одиничне
// поле) свідомо: якщо в майбутньому з'явиться другий незалежно
// оновлюваний файл (напр. окремий ресурс), формат не доведеться міняти
// і pack-manager/worker лишаються сумісні без правок.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
)

// Component — один файл, що входить у поставку лаунчера.
type Component struct {
	Path   string `json:"path"`   // відносний шлях від installDir, напр. "launcher.exe"
	SHA256 string `json:"sha256"` // hex-encoded
	Size   int64  `json:"size"`
	URL    string `json:"url"` // відносний до CDN base, напр. "/launcher/launcher.exe"
}

// Manifest — launcher-version.json, і локальний, і віддалений (CDN).
type Manifest struct {
	Version      string      `json:"version"`
	ReleaseNotes string      `json:"releaseNotes"`
	Components   []Component `json:"components"`
}

// Load читає локальний launcher-version.json. Відсутність файлу (перший
// запуск: немає ні launcher.exe, ні цього маніфесту в installDir) — НЕ
// помилка, повертаємо порожній Manifest{} і nil: виклик має трактувати
// це як "качати все з нуля".
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	// build.ps1/installer пишуть launcher-version.json з UTF-8 BOM
	// (U+FEFF), а Go json.Unmarshal на BOM падає. Якби не прибирали BOM,
	// маніфест вважався б пошкодженим -> "нічого не встановлено" ->
	// updater перекачував би launcher.exe при кожному запуску.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		// Пошкоджений локальний маніфест не повинен блокувати
		// оновлення назавжди — трактуємо як "нічого не встановлено".
		return &Manifest{}, nil
	}
	return &m, nil
}

// Save атомарно перезаписує launcher-version.json (пишемо у .tmp і
// переносимо — щоб крах процесу посеред запису не лишив биту чернетку,
// через яку наступний запуск подумав би, що нічого не встановлено, і
// перекачав все заново).
func Save(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Diff порівнює локальний і віддалений маніфест і повертає компоненти,
// які треба (пере)завантажити. Порівняння йде за SHA256+розмір, а не
// лише за рядком версії — це навмисно: дозволяє докачати конкретний
// пошкоджений/відсутній файл, навіть якщо version-рядок вже співпадає
// (напр. після невдалого попереднього оновлення, яке впало посередині).
func Diff(local, remote *Manifest, installDir string) []Component {
	localByPath := make(map[string]Component, len(local.Components))
	for _, c := range local.Components {
		localByPath[c.Path] = c
	}

	var toDownload []Component
	for _, rc := range remote.Components {
		lc, ok := localByPath[rc.Path]
		if !ok || lc.SHA256 != rc.SHA256 || lc.Size != rc.Size {
			toDownload = append(toDownload, rc)
			continue
		}
		// Маніфест каже "файл не змінився" — але перевіримо, що він
		// реально є на диску і має правильний хеш. Без цього
		// пошкоджений/видалений вручну launcher.exe ніколи б не
		// докачався, поки не вийде нова версія.
		full := installDir + string(os.PathSeparator) + rc.Path
		if ok2, _ := VerifyFile(full, rc.SHA256, rc.Size); !ok2 {
			toDownload = append(toDownload, rc)
		}
	}
	return toDownload
}

// VerifyFile перевіряє, що файл на диску має саме такі розмір і SHA256.
// Відсутність файлу — не помилка (повертає false, nil): виклику треба
// просто перезавантажити компонент, а не впасти з fatal error.
func VerifyFile(path, wantSHA256 string, wantSize int64) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, nil
	}
	if info.Size() != wantSize {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	got := hex.EncodeToString(h.Sum(nil))
	return got == wantSHA256, nil
}
